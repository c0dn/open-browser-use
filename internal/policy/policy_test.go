package policy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ifuryst/open-browser-use/internal/engine"
)

// fakePage serves fixed observations and records act calls.
type fakePage struct {
	pages []*engine.Observation
	acts  [][]engine.Step
}

func (f *fakePage) Observe(context.Context) (*engine.Observation, error) { return f.pages[0], nil }

func (f *fakePage) Act(_ context.Context, steps []engine.Step) (*engine.ActResult, error) {
	f.acts = append(f.acts, steps)
	if len(f.pages) > 1 {
		f.pages = f.pages[1:]
	}
	return &engine.ActResult{Observation: f.pages[0]}, nil
}

func formPage() *engine.Observation {
	return &engine.Observation{URL: "https://example.com/", Title: "Form", Marker: json.RawMessage(`[1]`),
		Elements: []engine.Element{
			{Index: 1, Node: 1, Role: "textbox", Label: "Name", Ops: []string{"type", "click"}},
			{Index: 2, Node: 2, Role: "checkbox", Label: "Agree", Ops: []string{"click"}, Checked: "false"},
			{Index: 3, Node: 3, Role: "button", Label: "Submit", Ops: []string{"click"}},
		}}
}

// gateway answers System One with the next queued answers and the text model with {"text": value}.
func gateway(t *testing.T, decisions []map[string]any, text string) (Config, *[]map[string]any) {
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing key on %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		switch r.URL.Path {
		case "/typesafe/v1/systemone":
			seen = append(seen, req)
			if len(decisions) == 0 {
				t.Fatal("unexpected decision")
			}
			json.NewEncoder(w).Encode(map[string]any{"model": "jev", "answers": decisions[0], "usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "cost": 0.001}})
			decisions = decisions[1:]
		case "/v1/responses":
			json.NewEncoder(w).Encode(map[string]any{"output": []any{map[string]any{"type": "message",
				"content": []any{map[string]any{"type": "output_text", "text": text}}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return Config{BaseURL: srv.URL, APIKey: "k", Model: "jev-latest", TextModel: "luna"}, &seen
}

func choice(c string) map[string]any { return map[string]any{"choice": c, "confidence": 0.9} }

func TestRunFillsFormAndFinishes(t *testing.T) {
	done := formPage()
	done.Marker = json.RawMessage(`[2]`)
	page := &fakePage{pages: []*engine.Observation{formPage(), done}}
	cfg, seen := gateway(t, []map[string]any{
		{"operation": choice("FILL_FORM"), "submit_target": choice("3"), "set_1": map[string]any{"noul": 0.9},
			"set_2": map[string]any{"noul": 0.8}, "click_target": choice("3"), "type_text_target": choice("1")},
		{"operation": choice("DONE")},
	}, `{"text": "William"}`)

	res, err := cfg.Run(context.Background(), page, "Name William, agree, submit", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusDone || res.Decisions != 2 {
		t.Fatalf("status %s after %d decisions: %s", res.Status, res.Decisions, res.Note)
	}
	want := []engine.Step{{Op: "type", Index: 1, Value: "William"}, {Op: "check", Index: 2, Value: "true"}, {Op: "click", Index: 3}}
	if len(page.acts) != 1 || len(page.acts[0]) != 3 {
		t.Fatalf("acts = %+v", page.acts)
	}
	for i, s := range want {
		if page.acts[0][i] != s {
			t.Errorf("step %d = %+v, want %+v", i, page.acts[0][i], s)
		}
	}
	q := (*seen)[0]["questions"].(map[string]any)
	ops := q["operation"].(map[string]any)["criteria"].(map[string]any)
	for _, op := range []string{"FILL_FORM", "CLICK", "TYPE_TEXT", "DONE", "NEEDS_OWNER"} {
		if _, ok := ops[op]; !ok {
			t.Errorf("operation %s not offered", op)
		}
	}
	if res.Cost != 0.002 {
		t.Errorf("cost = %v", res.Cost)
	}
}

func TestRunRejectsAnswersOutsideTheOffer(t *testing.T) {
	cfg, _ := gateway(t, []map[string]any{{"operation": choice("CLICK"), "click_target": choice("99")}}, "")
	_, err := cfg.Run(context.Background(), &fakePage{pages: []*engine.Observation{formPage()}}, "x", Options{})
	if err == nil || !strings.Contains(err.Error(), "invalid answer") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunStopsForPasswordsAndSpending(t *testing.T) {
	cfg, seen := gateway(t, nil, "")
	login := formPage()
	login.Elements = append(login.Elements, engine.Element{Index: 4, Node: 4, Role: "password", Label: "Password", Ops: []string{"password"}})
	res, err := cfg.Run(context.Background(), &fakePage{pages: []*engine.Observation{login}}, "sign in", Options{})
	if err != nil || res.Status != StatusNeedsOwner {
		t.Fatalf("password page: %v %+v", err, res)
	}
	res, err = cfg.Run(context.Background(), &fakePage{pages: []*engine.Observation{formPage()}}, "buy now the blue shoes", Options{})
	if err != nil || res.Status != StatusNeedsApproval {
		t.Fatalf("spend goal: %v %+v", err, res)
	}
	if len(*seen) != 0 {
		t.Errorf("asked Jev %d times before stopping", len(*seen))
	}
}

func TestRunStopsWhenNothingChanges(t *testing.T) {
	click := map[string]any{"operation": choice("CLICK"), "click_target": choice("3")}
	cfg, _ := gateway(t, []map[string]any{click, click, click}, "")
	res, err := cfg.Run(context.Background(), &fakePage{pages: []*engine.Observation{formPage()}}, "x", Options{})
	if err != nil || res.Status != StatusBlocked || len(res.Steps) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestRunReportsDoneAfterTheLastAllowedAction(t *testing.T) {
	opened := formPage()
	opened.Marker = json.RawMessage(`[2]`)
	click := map[string]any{"operation": choice("CLICK"), "click_target": choice("3")}
	cfg, seen := gateway(t, []map[string]any{click, {"operation": choice("DONE")}}, "")
	res, err := cfg.Run(context.Background(), &fakePage{pages: []*engine.Observation{formPage(), opened}}, "x", Options{MaxActions: 1})
	if err != nil || res.Status != StatusDone || len(res.Steps) != 1 {
		t.Fatalf("%v %+v", err, res)
	}

	cfg, seen = gateway(t, []map[string]any{click, click}, "")
	page := &fakePage{pages: []*engine.Observation{formPage(), opened}}
	res, err = cfg.Run(context.Background(), page, "x", Options{MaxActions: 1})
	if err != nil || res.Status != StatusLimit || len(res.Steps) != 1 || len(page.acts) != 1 || len(*seen) != 2 {
		t.Fatalf("%v %+v acts=%d decisions=%d", err, res, len(page.acts), len(*seen))
	}
}

func TestFieldTextNullMeansNeedsInfo(t *testing.T) {
	cfg, _ := gateway(t, []map[string]any{{"operation": choice("TYPE_TEXT"), "type_text_target": choice("1")}}, `{"text": null}`)
	res, err := cfg.Run(context.Background(), &fakePage{pages: []*engine.Observation{formPage()}}, "fill it", Options{})
	if err != nil || res.Status != StatusNeedsInfo {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestConfigFromEnvReadsKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/key"
	if err := writeFile(path, " pt_test \n"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OBU_POLICY_API_KEY", "")
	t.Setenv("OBU_POLICY_API_KEY_FILE", path)
	t.Setenv("OBU_POLICY_BASE_URL", "https://gw.example/v1/")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "pt_test" || cfg.BaseURL != "https://gw.example" || cfg.Model != "jev-latest" {
		t.Fatalf("%+v", cfg)
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }
