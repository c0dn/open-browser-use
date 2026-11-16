// Node test: a session keeps its tabs in browsers that do not keep extension tab groups.
// Run: node --test apps/chrome-extension/session-tabs.test.mjs
//
// Vivaldi 8.3 turns extension tab groups into its own tab stacks: chrome.tabs.group succeeds, but the group then
// reports no tabs. Sessions used to find their tabs only through the group, so every tab "was not part of" its
// session right after it was opened. A session now also counts the tabs it opened (tabOrigins) that still exist.

import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import vm from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const HERE = path.dirname(fileURLToPath(import.meta.url));

function fakeChrome({ keepsGroups }) {
  const tabs = new Map();
  let nextTab = 500;
  let nextGroup = 10;
  const storage = {};
  const listener = { addListener() {} };
  const chrome = {
    storage: {
      local: {
        async get(key) {
          return typeof key === "string" ? (key in storage ? { [key]: storage[key] } : {}) : { ...storage };
        },
        async set(obj) {
          Object.assign(storage, obj);
        }
      }
    },
    tabs: {
      async create({ url, windowId }) {
        const tab = { id: nextTab++, windowId, url, groupId: -1, active: false };
        tabs.set(tab.id, tab);
        return { ...tab };
      },
      async get(id) {
        const tab = tabs.get(id);
        if (!tab) throw new Error(`No tab with id: ${id}`);
        return { ...tab };
      },
      async query(filter = {}) {
        return [...tabs.values()].filter((t) => typeof filter.groupId !== "number" || t.groupId === filter.groupId).map((t) => ({ ...t }));
      },
      async group({ groupId, tabIds }) {
        const id = typeof groupId === "number" ? groupId : nextGroup++;
        // Vivaldi: the call succeeds, but the tab does not stay in the group.
        if (keepsGroups) for (const t of [tabIds].flat()) tabs.get(t).groupId = id;
        return id;
      },
      async remove(id) {
        tabs.delete(id);
      },
      onRemoved: listener,
      onUpdated: listener,
      onActivated: listener
    },
    tabGroups: {
      async get(id) {
        return { id };
      },
      async update(id) {
        return { id };
      },
      onRemoved: listener
    },
    windows: {
      async getAll() {
        return [{ id: 1, focused: true, type: "normal" }];
      },
      onRemoved: listener
    },
    debugger: { onEvent: listener, onDetach: listener },
    runtime: {
      onInstalled: listener,
      onStartup: listener,
      onSuspend: listener,
      onConnectExternal: listener,
      onMessage: listener,
      getManifest: () => ({ version: "test" })
    },
    alarms: { create: async () => {}, clear: async () => {}, onAlarm: listener },
    downloads: { onCreated: listener, onChanged: listener, onDeterminingFilename: listener },
    notifications: { create() {}, onClicked: listener }
  };
  return { chrome, tabs };
}

async function backend(chrome) {
  const source = await readFile(path.join(HERE, "background.js"), "utf8");
  const context = vm.createContext({
    chrome, console, setTimeout, clearTimeout, setInterval, clearInterval, queueMicrotask,
    URL, TextEncoder, TextDecoder, structuredClone, crypto: globalThis.crypto, Date
  });
  vm.runInContext(
    "chrome.runtime.connectNative = () => ({ onMessage: { addListener(){} }, onDisconnect: { addListener(){} }, postMessage(){}, disconnect(){} });",
    context
  );
  vm.runInContext(`${source}\nglobalThis.__backend = BrowserBackend;`, context);
  return new context.__backend();
}

for (const keepsGroups of [true, false]) {
  test(`a session keeps the tab it opened (browser ${keepsGroups ? "keeps" : "drops"} tab groups)`, async () => {
    const { chrome, tabs } = fakeChrome({ keepsGroups });
    const b = await backend(chrome);
    const params = { session_id: "s1", turn_id: "t1" };
    const tab = await b.createTab(params);
    const listed = await b.getTabs(params);
    assert.deepEqual(Array.from(listed, (t) => t.id), [tab.id]);

    // A closed tab leaves the session.
    tabs.delete(tab.id);
    assert.equal((await b.getTabs(params)).length, 0);
  });
}
