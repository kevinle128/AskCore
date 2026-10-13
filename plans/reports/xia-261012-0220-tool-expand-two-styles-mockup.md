# Mockup: hai kiểu mở rộng output tool

Phác thảo để so sánh, chưa phải design được duyệt. Khung 80 cột. Ba tool call trong cùng một phiên: `bash`, `read`, `edit`.

## Kiểu A: toggle chung `ctrl+o` (Pi)

Một phím đổi **tất cả** tool trong phiên, kể cả những tool đã lên scrollback. Ask vẽ lại toàn bộ scrollback (giống `ctrl+t` cho thinking hiện nay).

### A1. Thu gọn (mặc định)

```text
  ❯ chạy test rồi sửa lỗi trong parser.go                               14:02

  ● bash  go test ./...                                               ✗ exit 1
    … 41 dòng trước (ctrl+o để mở)
    --- FAIL: TestParse (0.00s)
        parser_test.go:22: want 3, got 2
    FAIL  example.com/p  0.012s
    FAIL

  ● read  parser.go                                                     ✓ 0.0s
    package parser
    
    import "strings"
    … 118 dòng nữa (ctrl+o để mở)

  ● edit  parser.go                                                ✓ +2 -1 0.0s

  ◆ Đã sửa lỗi đếm token trong parser.go.
```

### A2. Sau khi bấm `ctrl+o`: mọi tool đều mở

```text
  ● bash  go test ./...                                               ✗ exit 1
    ok    example.com/a  0.004s
    ok    example.com/b  0.006s
    … (đủ 45 dòng)
    FAIL

  ● read  parser.go                                                     ✓ 0.0s
    package parser
    … (đủ 121 dòng)

  ● edit  parser.go                                                ✓ +2 -1 0.0s
    @@ -20,3 +20,4 @@
       for _, f := range fields {
    -      n++
    +      if f != "" {
    +          n++
    +      }
    (ctrl+o để thu gọn)
```

Footer báo trong 2 giây: `tool output: mở`. Bấm lại `ctrl+o` thì tất cả thu gọn.

**Ưu:** chỉ có một phím, không cần học thêm và không cần chọn block. Dùng lại đường vẽ lại có sẵn, nên ít code và ít rủi ro.
**Nhược:** muốn xem một output dài thì mọi output khác cũng mở theo, nên scrollback rất dài. Mỗi lần bấm, scrollback được xóa rồi vẽ lại.

## Kiểu B: gập/mở từng block (Grok)

Mỗi tool có trạng thái riêng. Muốn mở một tool, trước hết phải **chọn** tool đó.

### B1. Vào chế độ chọn (ví dụ `ctrl+o`), con trỏ `▶` đứng trên tool cuối

```text
  ● bash  go test ./...                                               ✗ exit 1
    … 42 dòng nữa (Enter để mở)

  ● read  parser.go                                                     ✓ 0.0s
    … 121 dòng nữa (Enter để mở)

▶ ● edit  parser.go                                                ✓ +2 -1 0.0s
    … diff 5 dòng (Enter để mở)
```

```text
┊ chọn tool · ↑↓ di chuyển · Enter mở/gập · Esc thoát
```

### B2. Bấm `↑` hai lần lên `bash`, bấm Enter: chỉ `bash` mở

```text
▶ ● bash  go test ./...                                               ✗ exit 1
    ok    example.com/a  0.004s
    … (đủ 45 dòng)
    FAIL

  ● read  parser.go                                                     ✓ 0.0s
    … 121 dòng nữa (Enter để mở)

  ● edit  parser.go                                                ✓ +2 -1 0.0s
    … diff 5 dòng (Enter để mở)
```

**Ưu:** mở đúng tool cần xem, các tool khác vẫn gọn.
**Nhược:**
- Phải xây thêm chế độ chọn: con trỏ, phím, rail gợi ý, và xử lý editor mất focus.
- Ở chế độ inline, tool đã cuộn lên trên khung terminal thì không thấy con trỏ `▶`. Phải cuộn scrollback của terminal để tìm, trong khi phím `↑↓` lại đi theo thứ tự block. Grok tránh được vấn đề này vì dùng alt-screen và tự cuộn.
- Mỗi lần mở hay gập một block vẫn phải vẽ lại toàn bộ scrollback như kiểu A.

## Tóm tắt

| | A: chung (Pi) | B: từng block (Grok) |
|---|---|---|
| Phím | `ctrl+o` | `ctrl+o` để chọn, `↑↓`, Enter, Esc |
| Thứ được mở | tất cả tool | một tool |
| UI mới | một thông báo trên footer | chế độ chọn, con trỏ, rail |
| Hợp với inline | tốt | kém (tool đã cuộn khỏi màn hình thì khó chọn) |
| Khối lượng | nhỏ | lớn hơn nhiều |

Đề xuất: A. Kiểu B có thể làm sau trong T2, nếu cần (alt-screen tùy chọn D15).
