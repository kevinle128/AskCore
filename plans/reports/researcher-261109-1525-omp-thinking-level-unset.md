# omp và Pi xử lý thinking level khi người dùng chưa chọn và khi đổi model

Ngày: 2026-11-09 (Asia/Saigon). Chỉ đọc code, không chạy omp và không gọi mạng.

Mã đã đọc:
- omp: `origin/main` **21f1188cf88b2f534c092181968cd5a4d1a9c296** (2026-11-09), đọc qua worktree tạm `/tmp/omp-main` (đã xoá).
- Pi: `origin/main` **7d073328e44337d3292a7db9b0f0308f679c5e97** (2026-11-08), đọc qua worktree tạm `/tmp/pi-main` (đã xoá).

Đường dẫn bên dưới là đường dẫn tương đối trong repo. Đánh dấu **[suy luận]** cho kết luận tôi lần theo code mà không có test xác nhận. Các dòng còn lại là đọc trực tiếp.

## Kết luận chính

1. **Không có hệ nào biến "chưa chọn" thành `off` khi đổi model.** Đây chính là lỗi của AskCore hiện nay.
   - omp giữ nguyên selector hiện tại khi đổi model: unset vẫn là unset, `off` vẫn là `off`, mức cụ thể thì clamp xuống.
   - Pi không có trạng thái "chưa chọn". Khi đổi model, Pi lấy lại mức từ setting (per-model, rồi global), sau đó mới tới mức hiện tại, cuối cùng là `medium`, rồi clamp.
2. **Khi khởi động phiên, cả hai đều chọn một mức cụ thể, không để vendor tự quyết.** omp mặc định `high` (setting `defaultThinkingLevel`). Pi mặc định `medium`.
3. **`off` không bao giờ được hiểu là "bỏ trường để vendor dùng mặc định".** Cả hai luôn gửi một tín hiệu tắt rõ ràng. Nếu model không tắt được thì gửi mức thấp nhất.
4. **Extension hoặc hook đặt thinking đi qua đúng hàm mà người dùng dùng, nên clamp giống nhau** (đúng cho cả omp và Pi).
5. omp vẫn có vài đường gửi "không có trường reasoning" (vendor default): model có thinking `unverified`, selector `inherit`, và trường hợp level bị mất sau khi đi qua một model không reasoning **[suy luận]**. Riêng với Anthropic thì omp **không có** đường nào để vendor tự quyết: unset và `off` đều thành tắt.

## Câu 1. Khởi động phiên khi không có cờ `--thinking` và không có setting đã lưu

### omp

- Setting `defaultThinkingLevel` có mặc định là `"high"`. Giá trị hợp lệ là `minimal…max` và `auto`; không có `off`: `packages/coding-agent/src/session/settings.ts:238-251`.
- Thứ tự ưu tiên lúc khởi động nằm trong `pickInitialThinkingLevel` (`packages/coding-agent/src/sdk.ts:2307-2335`):
  1. tuỳ chọn truyền vào (`--thinking`);
  2. entry thinking đã lưu trong phiên;
  3. hậu tố `:level` của model được khôi phục;
  4. selector tường minh của role `default`;
  5. `model.thinking.defaultLevel`;
  6. setting toàn cục.

  Bước 4–6 là `newSessionThinkingLevel`: `packages/tui/src/thinking.ts:165-175`.
- Model có thinking `unverified` thì không nhận mức nào (giữ `undefined`): `automaticThinkingLevel`, `packages/tui/src/thinking.ts:139-141`.
- Mức được clamp theo ladder của model qua `resolveThinkingLevelForModel` (`packages/tui/src/thinking.ts:122-133`), hàm này gọi `clampThinkingLevelForModel` (`packages/catalog/src/model-thinking.ts:51-87`):
  - model không reasoning cho kết quả `undefined`;
  - mức không được hỗ trợ thì leo **xuống** mức thấp hơn gần nhất.
- `--thinking` nhận `off, minimal…max, auto` và từ chối `inherit`: `packages/tui/src/thinking.ts:9,263-266` và `packages/coding-agent/src/commands/launch-help.ts:80-83`.
- Từ selector sang wire: `toReasoningEffort` trả `undefined` cho `off`/`inherit`/unset, và `shouldDisableReasoning` trả true **chỉ** khi là `off` (`packages/tui/src/thinking.ts:105-117`). Agent nhận cả hai giá trị qua `packages/coding-agent/src/session/model-controls.ts:548-551`. Như vậy omp có ba trạng thái khác nhau: mức cụ thể, `off` (disableReasoning=true), và unset (không có effort, không có cờ tắt).

Giá trị gửi lên wire với mặc định `high`:

| Model | Wire |
|---|---|
| gpt-5.x (openai-responses) | `reasoning: { effort: "high", summary: "auto" }` (`packages/ai/src/providers/openai-shared.ts:4320-4339`) |
| Claude adaptive (Opus/Sonnet 4.6+) | `thinking: {type:"adaptive"}` cùng `output_config.effort: "high"` (`packages/ai/src/stream.ts:1872-1891`, `packages/ai/src/providers/anthropic.ts:5272-5288`) |
| Claude dùng budget | `thinking: {type:"enabled", budget_tokens: …}` |
| grok-4.7 (xAI, Responses) | `reasoning.effort: "high"`, không có summary vì xAI từ chối `reasoning.summary` (`packages/catalog/src/compat/rules/providers/xai.kdl:46-61`; `packages/ai/src/providers/openai-shared.ts:4263-4269`) |

**omp có bao giờ gửi "không có trường reasoning" không?** Có, nhưng chỉ khi selector là unset:
- **OpenAI Responses:** khi không có effort, không có cờ tắt và không có summary, `applyResponsesCompatPolicy` không ghi `params.reasoning`, nên vendor tự quyết (`packages/ai/src/providers/openai-shared.ts:4319-4340`).
  - Ngoại lệ: khi bật ẩn tóm tắt thinking (`hideThinkingSummary`), `reasoningSummary` là `null`, và nhánh này gửi `effort: "medium"` (dòng 4320-4321).
- **Anthropic:** unset được gập thành `thinkingEnabled: false` (`packages/ai/src/stream.ts:1843-1858`), tức là **tắt** chứ không phải mặc định của vendor (xem Câu 4).
- **Các tình huống tạo ra unset:** model `unverified`, selector `inherit` (extension API chấp nhận giá trị này), và model không reasoning.

### Pi

- `DEFAULT_THINKING_LEVEL = "medium"`: `packages/coding-agent/src/core/defaults.ts:3`.
- Thứ tự ưu tiên: `options.thinkingLevel`, rồi entry của phiên, rồi `getModelThinkingLevel` (per-model), rồi setting global, rồi `medium`. Sau đó clamp; nếu không có model thì dùng `off` (`packages/coding-agent/src/core/sdk.ts:253-278`).
- Pi không có trạng thái unset. Kiểu `ThinkingLevel` luôn có giá trị; model không reasoning chỉ hỗ trợ `["off"]` (`packages/ai/src/models.ts:1276-1284`).

## Câu 2. Đổi model giữa phiên khi level đang là mặc định hoặc chưa đặt

### omp

Mọi đường đổi model đều gọi `#reapplyThinkingLevel` (`packages/coding-agent/src/session/model-controls.ts:616-622`):

```ts
setThinkingLevel(autoThinking ? "auto" : automaticThinkingLevel(model, preferredDefault ?? this.#thinkingLevel))
```

Các đường gọi:
- `setModel`, dùng cho bộ chọn `/model` với phạm vi persist, cho RPC `set_model`, và cho `setModel` của extension. `preferredDefault = target.thinking.defaultLevel`: `model-controls.ts:306-345` (dòng 341-343).
- `setModelTemporary`, dùng cho bộ chọn model chỉ trong phiên. Mức tường minh thắng; nếu không có thì giống `setModel`: `model-controls.ts:356-386`. Bên gọi: `packages/coding-agent/src/modes/controllers/selector-controller.ts:766-776`.
- ctrl+p là `cycleRoleModels`, gọi `applyRoleModel`, rồi `setModel`. Nếu role có hậu tố `:level` tường minh thì mức đó được áp sau: `model-controls.ts:426-452` và `packages/coding-agent/src/modes/controllers/input-controller.ts:3059-3062`.
- Chu kỳ `cycle_model` / `--models`: với scoped model thì dùng mức của scoped model đó (`model-controls.ts:497`); với danh sách model khả dụng thì dùng `#reapplyThinkingLevel()` (`model-controls.ts:526-527`).

Hệ quả:
- **Unset vẫn là unset.** `setThinkingLevel(undefined)` cho ra `undefined` (`model-controls.ts:589-599`). Không bao giờ thành `off`.
- **`off` vẫn là `off`.** `resolveThinkingLevelForModel` giữ nguyên `off` với mọi model (`packages/tui/src/thinking.ts:129-131`). Việc tắt thật sự được quyết ở adapter của provider (Câu 4). Vì vậy model không tắt được vẫn giữ selector `off`, còn wire thì gửi mức thấp nhất.
- **Mức cụ thể mà model mới không hỗ trợ:** clamp xuống mức thấp hơn gần nhất; nếu không có thì lấy mức thấp nhất của model (`packages/catalog/src/model-thinking.ts:73-86`). Có test: `packages/coding-agent/test/agent-session-role-thinking.test.ts:247,273`.
- **Ngoại lệ:** nếu model đích có `thinking.defaultLevel`, mức này **thay thế** mức hiện tại, kể cả `off`. Test `packages/coding-agent/test/issue-775-repro.test.ts:67-98` xác nhận việc nhận defaultLevel và việc giữ mức khi model không có defaultLevel. GPT, Claude và Grok first-party không khai báo defaultLevel trong KDL; chỉ một số model GLM, Kimi, Factory Droid và các model trong `_collapse` có.
- **Clamp làm mất thông tin.** Biến `#thinkingLevel` lưu mức *đã clamp* (`model-controls.ts:598`). Ví dụ: `xhigh` → sang model chỉ có tới `high` → quay lại model có `xhigh` thì vẫn là `high`. **[suy luận]**
- **Đi qua model không reasoning thì mất level.** `clampThinkingLevelForModel` trả `undefined` cho model không reasoning, nên khi quay về gpt-5.5 level vẫn là unset. Lúc đó OpenAI không nhận trường reasoning nào (vendor default), còn Anthropic thì bị tắt. **[suy luận, không tìm thấy test]**
- Phiên ở chế độ `auto` thì vẫn giữ `auto` qua mọi lần đổi model.

### Pi

`_getThinkingLevelForModelSwitch` (`packages/coding-agent/src/core/agent-session.ts:2828-2840`) chọn mức theo thứ tự:
1. mức tường minh của scoped model;
2. per-model setting;
3. `settingsManager.getDefaultThinkingLevel()`;
4. `this.thinkingLevel`;
5. `"medium"`.

Sau đó `setThinkingLevel` clamp mức này (`agent-session.ts:2771-2795`). Các đường gọi: `setModel` (`2644-2662`), cycle scoped (`2711`), cycle available (`2744`).

Hệ quả:
- Nếu đã có global default, **mỗi lần đổi model đều quay về giá trị setting**. Lựa chọn tạm thời trong phiên (chưa persist) bị bỏ đi. Chỉ khi không có setting thì Pi mới giữ mức hiện tại (đã clamp). **[suy luận từ thứ tự `??`]**
- Clamp của Pi thử **lên trước**, rồi mới xuống (`packages/ai/src/models.ts:1287-1305`). Model không tắt được (`thinkingLevelMap.off: null`) đổi `off` thành `minimal`; nếu `minimal` cũng không có thì thành `low`.

## Câu 3. Extension hoặc hook đặt thinking giữa phiên

### omp

- API là `setThinkingLevel(level: ThinkingLevel)`, trong đó `ThinkingLevel` gồm `inherit` và `off`: `packages/coding-agent/src/extensibility/extensions/types.ts:2167-2173`.
- Mọi binding đều gọi `session.setThinkingLevel`, sau đó tới `ModelControls.setThinkingLevel`. Đây cũng là hàm mà bộ chọn và lệnh slash dùng:
  - `packages/coding-agent/src/modes/controllers/extension-ui-controller.ts:281-282`
  - `packages/coding-agent/src/modes/runtime-init.ts:133-134`
  - `packages/coding-agent/src/modes/acp/acp-agent.ts:3297`
  - RPC `set_thinking_level`: `packages/coding-agent/src/modes/rpc/rpc-mode.ts:2550-2552`
- Vì vậy clamp giống hệt: `off` giữ `off` và được tắt ở adapter; mức cụ thể clamp xuống; `inherit` thành unset (vendor default trên OpenAI, tắt trên Anthropic).

### Pi

- `setThinkingLevel: (level) => this.setThinkingLevel(level)`: `packages/coding-agent/src/core/agent-session.ts:3589`. Cùng clamp với người dùng; `off` trên model không tắt được sẽ leo lên `minimal` hoặc `low`.

## Câu 4. Ý nghĩa của `off` và `minimal` trên wire theo provider

### omp

| Provider / model | `off` | unset |
|---|---|---|
| OpenAI first-party gpt-5.2–5.5 (Responses) | `reasoning.effort: "low"`, là mức thấp nhất trong ladder. Disable mode mặc định là `lowest-effort` (`packages/catalog/src/compat/resolve.ts:301-319`; ladder `low…xhigh` ở `packages/catalog/src/compat/rules/classes/openai.kdl:40-42`). Chỉ thế hệ 5.6 mới dùng `"none"` (`packages/catalog/src/compat/rules/providers/openai.kdl:120-152`; ghi chú xác nhận ở `packages/catalog/src/compat/rules/providers/commandcode.kdl:69-74`). Logic chọn giá trị: `packages/ai/src/providers/openai-shared.ts:1063-1083,4302-4317`. | không có trường reasoning |
| OpenAI Codex (subscription) | `none` nếu model hỗ trợ, nếu không thì mức thấp nhất. Không bao giờ bỏ trường, vì bỏ trường thì backend dùng mức mặc định của nó (`packages/ai/src/providers/openai-codex-responses.ts:1360-1374`; `packages/ai/CHANGELOG.md:405`). | không có effort |
| xAI grok-4.6/4.7 | Ladder là `minimal…xhigh`, với ánh xạ `minimal→"low"`, nên `off` gửi `reasoning.effort: "low"` (`packages/catalog/src/compat/rules/providers/xai.kdl:58-66`, cộng với `lowest-effort`). grok-code-fast, grok-build và SKU có hậu tố `*reasoning` không gửi effort trong mọi trường hợp (`xai.kdl:69-72`). | không có trường reasoning |
| Anthropic budget | `thinking: {type:"disabled"}` (`packages/ai/src/providers/anthropic.ts:5319-5320`) | giống `off` |
| Anthropic adaptive-only (Opus/Sonnet 4.6+) | Bỏ trường `thinking` (API mặc định là adaptive) và đặt `output_config.effort: "low"`; Sonnet 5.5 thì dùng `between_tools` (`packages/ai/src/providers/anthropic.ts:5301-5318`) | giống `off` (`packages/ai/src/stream.ts:1849`) |
| Model bắt buộc reasoning (`requiresEffort`) | Clamp lên `defaultSupportedEffort` (`packages/ai/src/stream.ts:1736-1751`) | giống `off` |

`minimal` là một effort thật. Adapter ánh xạ hoặc clamp nó khi vendor không có tier này:
- Anthropic: `minimal→low` (`packages/catalog/src/model-thinking.ts:133-156`);
- xAI: `minimal→"low"`;
- gpt ≥5.2: ladder không có `minimal`, nên session clamp xuống rồi lấy `low`.

### Pi

| Provider / model | `off` |
|---|---|
| gpt-5.x (Responses) | `thinkingLevelMap.off = null` (`packages/ai/catalog/families/openai.ts:87-90`), nên session clamp `off` lên `minimal` và wire gửi `"minimal"`. Với model có `off` không phải null, Responses gửi `effort: thinkingLevelMap.off ?? "none"` (`packages/ai/src/api/openai-responses.ts:410-413`). |
| xAI grok (Responses) | `off: null, minimal: null` (`packages/ai/catalog/providers/xai.ts:25-26`), nên dùng `low`. |
| Anthropic | `thinking: {type:"disabled"}` (`packages/ai/src/api/anthropic-messages.ts:1539`). Các model có `off: null` (`packages/ai/catalog/families/anthropic.ts:40,108,126`) thì clamp lên. |

## Bảng so sánh: tình huống, omp, Pi

| Tình huống | omp | Pi |
|---|---|---|
| Khởi động, không cờ, không setting | `high` (setting mặc định), clamp xuống theo ladder. Model `unverified` hoặc không reasoning thì unset. | `medium`, clamp lên trước. Model không reasoning thì `off`. |
| Khởi động, setting là `auto` | `auto` (mức tạm thời là defaultLevel hoặc High, tối đa XHigh) | không có `auto` |
| Đổi model, level đang unset | giữ unset (OpenAI và xAI: vendor default; Anthropic: tắt) | không áp dụng (Pi không có unset); lấy lại từ setting, hoặc mức hiện tại, hoặc `medium` |
| Đổi model, level `off` | giữ `off` (trừ khi model đích có `defaultLevel`); adapter gửi mức thấp nhất nếu model không tắt được | setting (nếu có) thắng; nếu không thì `off` được clamp lên `minimal`/`low` khi model không tắt được |
| Đổi model, mức cụ thể không được hỗ trợ | clamp xuống, lưu mức đã clamp (mất thông tin) | clamp lên trước, rồi xuống; setting global ghi đè lựa chọn tạm thời |
| Model đích có `thinking.defaultLevel` | defaultLevel thắng (kể cả khi đang `off`) | không có khái niệm này (chỉ có per-model setting của người dùng) |
| Extension đặt `off` | cùng đường với người dùng: giữ `off`, adapter tắt hoặc gửi mức thấp nhất | cùng đường: clamp |
| Extension đặt `inherit` | unset, giống hàng "unset" | không có |
| gpt-5.5 `off` trên wire | `effort: "low"` | `effort: "minimal"` |
| grok-4.7 `off` trên wire | `effort: "low"` | `effort: "low"` |
| Claude adaptive `off` trên wire | bỏ `thinking`, `effort: "low"` | clamp lên (vì `off: null`) |

## Khuyến nghị cho AskCore

**(a) Khởi động phiên mà không có level.**
- Cả omp và Pi đều chọn một mức cụ thể từ setting, không để vendor tự quyết. omp là `high`, Pi là `medium`.
- Nếu AskCore muốn giữ trạng thái "rỗng = vendor default", lý do đó vẫn hợp lệ cho OpenAI và xAI. Nhưng nên ghi rõ rằng với Anthropic thì "rỗng" và `off` là một (omp gập unset thành tắt; Anthropic budget mặc định là không thinking).
- Đề xuất: giữ "rỗng" lúc khởi động như hiện tại (giống đường unset của omp). Ngoài ra nên cho phép một setting mặc định cụ thể, như omp, để người dùng chọn.

**(b) Đổi model khi level đang rỗng.** Chọn một trong hai quy tắc, đừng trộn:
- **Quy tắc omp (giữ nguyên):** rỗng vẫn rỗng, `off` vẫn `off` (clamp ở adapter), mức cụ thể clamp theo model mới. Quy tắc này sửa đúng lỗi "rỗng thành off". Nó cũng thống nhất với đường extension, vì cả hai cùng gọi một hàm `setThinkingLevel`.
  - Nên cải tiến so với omp: lưu **lựa chọn gốc** của người dùng tách khỏi mức đã clamp, để tránh mất thông tin khi đi qua model yếu hơn hoặc model không reasoning.
- **Quy tắc Pi (lấy lại từ setting):** chỉ hợp khi AskCore bỏ trạng thái rỗng. Nhược điểm: lựa chọn trong phiên bị setting ghi đè ở mỗi lần đổi model.

Đề xuất: **dùng quy tắc omp (giữ nguyên selector, kể cả rỗng)**. Đồng thời sửa đường extension: hook đặt `off` phải ra `off` (clamp ở adapter), không ra rỗng. omp làm như vậy vì extension gọi chính `ModelControls.setThinkingLevel`.

**Chênh lệch cần quyết định:** AskCore gửi `none` cho gpt-5.5 khi `off`, còn omp gửi `low` (omp cho rằng chỉ 5.6 nhận `none` trên first-party) và Pi gửi `minimal`. Nên kiểm tra lại với API thật trước khi đổi.

## Câu hỏi còn mở

- Chưa có test nào xác nhận hai hành vi omp mất thông tin: đi qua model không reasoning thì mất level, và xhigh→high không phục hồi. Đây là kết luận lần theo code.
- Chưa xác minh với API thật xem gpt-5.5 có nhận `reasoning.effort: "none"` hay không. omp và Pi có quan điểm khác nhau.

Status: DONE
Summary: Cả omp và Pi đều không biến "chưa chọn" thành `off` khi đổi model. omp giữ nguyên selector (unset, off, hoặc mức đã clamp) qua cùng một hàm dùng cho người dùng và extension. Pi không có unset và lấy lại mức từ setting. Đề xuất AskCore làm theo quy tắc giữ nguyên selector của omp.
Concerns/Blockers: Hành vi mất level sau khi đi qua model không reasoning là suy luận, chưa có test. Giá trị wire `off` của gpt-5.5 khác nhau giữa AskCore (`none`), omp (`low`) và Pi (`minimal`).
