// Run with Node 22.19 or later. This reads the pinned Pi source; it does not change it.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { stripTypeScriptTypes } from "node:module";
import { pathToFileURL } from "node:url";

const root = process.argv[2] ?? "/Users/dale/Desktop/workspace/opensources/pi";
const sourceURL = (path) => pathToFileURL(`${root}/${path}`).href;
const { EventStream } = await import(sourceURL("packages/ai/src/utils/event-stream.ts"));
const { normalizeContext } = await import(sourceURL("packages/ai/src/utils/transcript.ts"));
const { getSystemMessageText } = await import(sourceURL("packages/ai/src/utils/text.ts"));
const { toJsonEvent } = await import(sourceURL("packages/coding-agent/src/modes/json-event.ts"));

// Keep the actual faux implementation. Remove module imports only; load its two
// runtime helpers from the source above. The provider registry is not exercised.
const fauxSource = await readFile(`${root}/packages/ai/src/providers/faux.ts`, "utf8");
const fauxCode = stripTypeScriptTypes(fauxSource.replace(/^import[\s\S]*?;\n/gm, ""));
const fauxModule = await import(`data:text/javascript,${encodeURIComponent(
  `import { createAssistantMessageEventStream } from ${JSON.stringify(sourceURL("packages/ai/src/utils/event-stream.ts"))};
   import { getSystemMessageText } from ${JSON.stringify(sourceURL("packages/ai/src/utils/text.ts"))};
   ${fauxCode}`,
)}`);
const { createFauxCore, fauxAssistantMessage, fauxText, fauxThinking, fauxToolCall } = fauxModule;
const consume = async (stream) => {
  const events = [];
  for await (const event of stream) events.push(structuredClone(event));
  return { events, result: await stream.result() };
};

const incomplete = new EventStream(() => false, (value) => value);
incomplete.end();
let resolved = false;
incomplete.result().then(() => { resolved = true; });
await new Promise((resolve) => setImmediate(resolve));
assert.equal(resolved, false, "Pi end() without a result does not settle result()");

const context = normalizeContext({
  systemPrompt: "sys",
  messages: [{ role: "user", content: "hi", timestamp: 1 }],
  tools: [{ name: "echo", description: "Echo", parameters: { type: "object" } }],
});
assert.equal(context.messages[0].toolsAdded[0].name, "echo");
assert.equal("tools" in context, false);
assert.equal(getSystemMessageText({ role: "system", content: "sys", sections: { b: "B", a: "A", c: null } }), "sys\n\nB\n\nA");

const core = createFauxCore({ api: "faux:test", tokenSize: { min: 1, max: 1 } });
const model = core.getModel();
core.setResponses([fauxAssistantMessage([
  fauxThinking("go"), fauxText("ok"), fauxToolCall("echo", {}, { id: "a" }), fauxToolCall("echo", {}, { id: "b" }),
], { stopReason: "toolUse", timestamp: 1 })]);
const { events, result } = await consume(core.stream(model, context));
assert.deepEqual(events.map((event) => event.type), [
  "start", "thinking_start", "thinking_delta", "thinking_end", "text_start", "text_delta", "text_end",
  "toolcall_start", "toolcall_delta", "toolcall_end", "toolcall_start", "toolcall_delta", "toolcall_end", "done",
]);
assert.equal(events[0].partial.stopReason, "pending");
assert.equal(result.stopReason, "toolUse");
const update = toJsonEvent({ type: "message_update", message: result, assistantMessageEvent: events[7] });
assert.deepEqual(update.assistantMessageEvent, { type: "toolcall_start", contentIndex: 2, id: "a", toolName: "echo" });
assert.deepEqual(update.usage, result.usage);
assert.equal("message" in update, false);

const emptyContext = normalizeContext({ messages: [] });
core.setResponses([fauxAssistantMessage("😀😀😀")]);
assert.equal((await consume(core.stream(model, emptyContext))).result.usage.output, 2, "Pi counts UTF-16 units, not runes");

core.setResponses([fauxAssistantMessage("ok"), fauxAssistantMessage("ok"), fauxAssistantMessage("ok")]);
const first = (await consume(core.stream(model, context, { sessionId: "s" }))).result.usage;
const same = (await consume(core.stream(model, context, { sessionId: "s" }))).result.usage;
const disabled = (await consume(core.stream(model, context, { sessionId: "s", cacheRetention: "none" }))).result.usage;
assert.ok(first.cacheWrite > 0);
assert.equal(first.totalTokens, first.input + first.output + first.cacheRead + first.cacheWrite);
assert.equal(same.input, 0);
assert.equal(same.cacheWrite, 0);
assert.equal(same.cacheRead, first.input);
assert.equal(disabled.cacheRead + disabled.cacheWrite, 0);

const controller = new AbortController();
controller.abort();
core.setResponses([fauxAssistantMessage("never")]);
const aborted = await consume(core.stream(model, context, { signal: controller.signal }));
assert.deepEqual(aborted.events.map((event) => event.type), ["error"]);
assert.equal(aborted.result.stopReason, "aborted");

const anthropicSource = await readFile(`${root}/packages/ai/src/api/anthropic-messages.ts`, "utf8");
const sseSource = anthropicSource.slice(anthropicSource.indexOf("interface ServerSentEvent"), anthropicSource.indexOf("async function* iterateAnthropicEvents"));
const { iterateSseMessages } = await import(`data:text/javascript,${encodeURIComponent(stripTypeScriptTypes(`${sseSource}\nexport { iterateSseMessages };`))}`);
const readSSE = async (chunks) => {
  const body = new ReadableStream({ start(controller) {
    for (const chunk of chunks) controller.enqueue(new TextEncoder().encode(chunk));
    controller.close();
  } });
  const events = [];
  for await (const event of iterateSseMessages(body)) events.push(event);
  return events;
};
assert.equal((await readSSE(["\uFEFFdata: ok\n\n"]))[0].data, "ok", "Pi TextDecoder removes the BOM");
assert.equal((await readSSE(["event: error\n\n"]))[0].data, "", "Pi dispatches a named event without data");
const unsplit = await readSSE(["data: a\r\ndata: b\r\n\r\n"]);
const split = await readSSE(["data: a\r", "\ndata: b\r\n\r\n"]);
assert.deepEqual(unsplit.map((event) => event.data), ["a\nb"]);
assert.deepEqual(split.map((event) => event.data), ["a", "b"], "Pi splits CRLF across reads into two line ends");

console.log("PASS: source stream settlement, transcript tools/order, faux order, JSON projection, UTF-16 usage, cache, abort, SSE BOM, event-only dispatch, and split-CRLF defect");
