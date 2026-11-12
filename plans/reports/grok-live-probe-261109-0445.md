# Đo trực tiếp hành vi grok trên xAI Responses: FORK hay UPSTREAM

Ngày đo: 2026-11-09 (Asia/Saigon). Credential: OAuth xAI đã lưu trong `~/.ask/auth.json` (method `xai-oauth`, endpoint `https://api.x.ai/v1`). Tổng cộng 12 lời gọi live: 4 qua `ask -p`, 8 gọi thẳng API (2 lời gọi bị 429 do xAI hết capacity).

## Kết luận

FORK (e894229) đúng cho grok. Kết luận theo từng trường:

| Trường | FORK | UPSTREAM | Kết quả đo | Kết luận |
|---|---|---|---|---|
| Role của system prompt | `system` | `developer` | xAI trả 200 cho cả hai role, trên grok-4.7, grok-4.20-0309-reasoning và grok-4.20-0309-non-reasoning. Ở cùng effort, câu trả lời giống nhau giữa hai biến thể. | Hai cách đều đúng trên wire. Không có khác biệt đo được. Mọi ví dụ trong docs xAI dùng `system`. |
| `reasoning.effort` | gửi | gửi | Hai biến thể gửi y hệt nhau cho grok-4.7 (`high` → `high`; `off` → không gửi trường `reasoning`). | Như nhau. Nhưng xem rủi ro về grok-4.20 bên dưới. |
| `temperature` / `top_p` | giữ | bỏ, kèm warning | xAI nhận `temperature: 0.3` và `top_p: 0.9` cùng `reasoning.effort: high` trên grok-4.7 (200) và trả lại đúng hai giá trị đó. grok-4.20-0309-reasoning cũng nhận `temperature`. | FORK đúng. UPSTREAM bỏ hai tham số mà xAI chấp nhận, nên người dùng mất quyền điều khiển mà không có lý do. |

Hành vi của từng biến thể đã được kiểm tra trong mã nguồn. Ở e894229, `providers/openai/responses_language_model.go:350` chỉ xoá `Temperature`/`TopP` khi `isReasoningModel`, và `resolveResponsesModelConfig` trả `isReasoningModel: false` cho id `grok-*` (dòng 175). Effort được gửi khi `acceptsReasoningOptions` (dòng 112, 326), giá trị này đúng cho mọi id không phải gpt-3, gpt-4, chatgpt, davinci, babbage, gpt-5-chat. Ở e18c879, `isResponsesReasoningModel` trong `providers/openai/reasoning.go:27` chỉ loại các id khớp `gpt-3|gpt-4|chatgpt-|-chat`, nên mọi grok có `systemMessageMode: "developer"` và bị xoá `Temperature`/`TopP` (`responses_language_model.go:262-273`).

Vì Ask hiện không bao giờ tự đặt `temperature` (`buildResponsesCall` trong `internal/providers/openai/responses_prompt.go` không đặt `Call.Temperature`), các lần chạy qua Ask chỉ phân biệt được hai biến thể ở role. Kết luận về `temperature` dựa trên lời gọi trực tiếp và docs. Với Ask hôm nay, chuyển sang UPSTREAM chỉ đổi role sang `developer`, và đo cho thấy việc đó vô hại. Với tính đúng của fantasy upstream, quy tắc "mọi id Responses là reasoning model" sai cho xAI ở phần `temperature`/`top_p`.

Rủi ro chung cho cả hai biến thể: grok-4.20-0309-reasoning và grok-4.20-0309-non-reasoning trả 400 khi request có `reasoning.effort`. Cả FORK (id lạ vẫn nhận reasoning options) và UPSTREAM (mọi grok là reasoning model) đều gửi effort khi caller đặt nó. Vì vậy việc chặn phải nằm ở catalog của Ask (`Reasoning`, `ThinkingLevelMap`), không phải ở fantasy. Trong `internal/providers/catalog_snapshot.json`, grok-4.20-0309-reasoning có `"reasoning": true`; nếu model này được chọn được trong Ask, một mức thinking khác `off` có thể gây 400.

## Các lần chạy qua Ask

Lệnh (stdin là `/dev/null`):

```sh
ASK_CAPTURE=<dir> go run ./cmd/tui -p "What is the capital of France?" --provider xai --model grok-4.7 --thinking <off|high> \
  --system-prompt "Always answer with the single word PINEAPPLE." -nt --mode json
```

Biến thể UPSTREAM: `replace charm.land/fantasy => /tmp/fantasy-e18c879` (git worktree của fantasy tại e18c879), chạy với `GOTOOLCHAIN=go1.27.3 GOFLAGS=-mod=mod`. Sau đó đã khôi phục `go.mod` và `go.sum` bằng `git checkout`.

"Tuân theo" nghĩa là câu trả lời chỉ là `PINEAPPLE`.

| Biến thể | Thinking | Role gửi | Effort gửi | Temperature gửi | HTTP | Tuân theo system prompt | Reasoning tokens | xAI trả lại |
|---|---|---|---|---|---|---|---|---|
| FORK | off | `system` | không gửi | không gửi | 200 | Có (`PINEAPPLE`) | 104 | effort `null`, temperature 0.7, top_p 0.95 |
| FORK | high | `system` | `high` | không gửi | 200 | Không (`Paris.`) | 383 | effort `high`, temperature 0.7, top_p 0.95 |
| UPSTREAM | off | `developer` | không gửi | không gửi | 200 | Có (`PINEAPPLE`) | 104 | effort `null`, temperature 0.7, top_p 0.95 |
| UPSTREAM | high | `developer` | `high` | không gửi | 200 | Không (`Paris.`) | 446 | effort `high`, temperature 0.7, top_p 0.95 |

Ghi chú:

- Mỗi request còn có hai message `user`: lời nhắc context file (AGENTS.md của worktree) và câu hỏi. Hai biến thể có cùng nội dung này.
- Ask chỉ biết grok-4.7 cho xAI. `grok-4.20-0309-non-reasoning`, `grok-4.20-0309-reasoning`, `grok-4.6`, `grok-4.3` đều báo `unknown model` trước khi gọi mạng, dù `~/.ask/model-cache.json` có các id này. Vì vậy id grok thứ hai chỉ được đo bằng lời gọi trực tiếp.
- `--thinking off` trên grok-4.7 không gửi trường `reasoning`. `TestGrok47SendsItsEffort` mong đợi `off` thành `low`, nhưng test đó gọi `ClampThinkingLevel` trực tiếp. Tôi chưa tìm nguyên nhân của khác biệt này (xem câu hỏi mở).
- Không gửi effort vẫn có 104 reasoning tokens. Docs nói grok-4.7 không tắt được reasoning.

## Lời gọi API trực tiếp

Một test tạm trong `cmd/tui` (đã xoá) lấy credential qua `app.NewNativeAuth` và `Service.Resolve`, giống `ask -p`, rồi POST JSON thô tới `https://api.x.ai/v1/responses` với `stream: false`, `store: false`. Input: role được chọn với "Always answer with the single word PINEAPPLE.", rồi `user` "What is the capital of France?". Token không bao giờ được in hay ghi ra file.

| Model | Role | Effort | temperature / top_p | HTTP | Trả lời | Reasoning tokens | xAI trả lại |
|---|---|---|---|---|---|---|---|
| grok-4.7 | `system` | `high` | 0.3 / 0.9 | 200 | `Paris.` | 725 | temperature 0.3, top_p 0.9, effort `high` |
| grok-4.7 | `developer` | `high` | 0.3 / 0.9 | 200 | `Paris.` | 378 | temperature 0.3, top_p 0.9, effort `high` |
| grok-4.7 | `system` | không | 0.3 / không | 429 (2 lần) | — | — | `resource-exhausted`: model hết capacity |
| grok-4.20-0309-non-reasoning | `developer` | không | 0.3 / không | 200 | `PINEAPPLE` | 0 | temperature 0.3, top_p 0.95, effort `null` |
| grok-4.20-0309-non-reasoning | `system` | `low` | không | 400 | — | — | `invalid-argument`: "Model grok-4.20-0309-non-reasoning does not support parameter reasoningEffort." |
| grok-4.20-0309-reasoning | `developer` | `high` | 0.3 / không | 400 | — | — | `invalid-argument`: "Model grok-4.20-0309-reasoning does not support parameter reasoningEffort." |
| grok-4.20-0309-reasoning | `developer` | không | 0.3 / không | 200 | `PINEAPPLE` | 1636 | temperature 0.3, top_p 0.95, effort `medium` |

Các sự thật rút ra:

1. xAI nhận `temperature` cùng `reasoning.effort` trên grok-4.7 và trả lại đúng giá trị đã gửi. Không có lỗi, không có cảnh báo.
2. xAI nhận role `developer` trên cả ba model đã thử.
3. Hai model grok-4.20 từ chối `reasoning.effort` bằng HTTP 400, kể cả model có reasoning.
4. Việc tuân theo system prompt phụ thuộc vào effort, không phụ thuộc vào role. Với effort `high`, grok-4.7 trả lời `Paris.` 4/4 lần (2 role, 2 đường gọi). Không có effort, grok-4.7 tuân theo 2/2 lần. Mẫu này nhỏ.

## Docs của xAI

- Role: OpenAPI spec, schema `ModelInputPart` của `/v1/responses`: "Possible values are `user`, `assistant`, `system` and `developer`." https://docs.x.ai/openapi.json. Docs không nói hai role khác nhau thế nào. Mọi ví dụ Responses dùng `system`, ví dụ https://docs.x.ai/developers/model-capabilities/text/generate-text và https://docs.x.ai/developers/model-capabilities/text/reasoning.
- Effort: "`grok-4.7`, `grok-4.6`, and `grok-4.5` support the `reasoning_effort` parameter" và "If not specified, `reasoning_effort` defaults to `"high"`. Reasoning cannot be disabled." https://docs.x.ai/developers/model-capabilities/text/reasoning. grok-4.7 nhận low, medium, high (mặc định), xhigh: https://docs.x.ai/developers/grok-4-7. Docs không nói về effort cho grok-4.20. Lỗi 400 ở trên là bằng chứng duy nhất.
- Tham số bị từ chối: "`presencePenalty`, `frequencyPenalty`, and `stop` cannot be used with reasoning models. Requests that include them return an error." (trang Reasoning ở trên). `temperature` và `top_p` không nằm trong danh sách này.
- Temperature: "Sampling uses at most `temperature` 1 and at most the model's default `top_p`. Requests may send `temperature` from 0 to 2 and `top_p` up to 1, and the response echoes the values you sent. A value above a cap is sampled as that cap." https://docs.x.ai/developers/rest-api-reference/inference/responses. Docs giới hạn hai giá trị này, không từ chối chúng.

Một điểm lệch nhỏ: docs nói mặc định của grok-4.7 là `high`, nhưng khi không gửi effort, xAI trả lại `effort: null` và chỉ 104 reasoning tokens (so với 383–725 ở `high`). Có thể mặc định thực tế thấp hơn `high`.

## Ghi chú cho việc sync fork

- Nếu sync theo UPSTREAM, Ask hôm nay vẫn chạy được với grok (role `developer` hợp lệ). Fantasy upstream sẽ bỏ `temperature`/`top_p` cho grok, điều này sai với xAI. Nếu giữ hành vi FORK cho id không phải OpenAI, `temperature` vẫn tới xAI.
- `SamplingParams` của `models.json` đi qua `ExtraBody`, nên một `temperature` ở đó tới wire với cả hai biến thể. Quy tắc reasoning model của fantasy không chặn được đường này.
- Ask chỉ gửi `reasoning.effort`, không bao giờ gửi `reasoning.summary` (thấy trong mọi capture). Vì vậy phần "summary" trong hành vi của FORK chưa được Ask dùng hôm nay.
- e18c879 cần `go >= 1.27.3`. Với `-mod=mod`, nó nâng chỉ thị `go` của AskCore lên 1.27.3 và nâng một số dependency (ví dụ otel 1.46 → 1.47). Thay đổi này đã được hoàn lại; nó chỉ cho biết chi phí của việc sync.

## Dọn dẹp

- Đã xoá test tạm `cmd/tui/zz_probe_xai_live_test.go` và khôi phục `go.mod`/`go.sum`. Đã gỡ worktree `/tmp/fantasy-e18c879`.
- Đã quét 20 file trong `/tmp/grok-probe` với 20 chuỗi credential của `auth.json` và các mẫu `Authorization`, `Bearer`, JWT, `xai-`, `sk-`: 0 kết quả. Sau đó đã xoá `/tmp/grok-probe`.

## Câu hỏi mở

1. Vì sao `ask -p --thinking off` trên grok-4.7 không gửi effort, trong khi `TestGrok47SendsItsEffort` mong đợi `low`?
2. Vì sao `ask -p` báo `unknown model` cho các id grok khác dù model cache có chúng?
3. Có nên chặn effort cho grok-4.20-0309-reasoning trong catalog của Ask trước khi model này chọn được không?
