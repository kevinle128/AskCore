Output H2-D-review.mp4

Set Shell "bash"
Set FontSize 18
Set Width 1300
Set Height 720
Set TypingSpeed 35ms
Set Padding 20

Hide
Type `cd /tmp/review/d && export PS1='$ ' && clear`
Enter
Show

Type "# H2-D review. Lane 1: trunk has no print mode (bubbletea menu opens)"
Enter
Type "./ask-trunk -p hello; echo exit=$?"
Enter
Sleep 2.5s
Ctrl+C
Sleep 1s
Type "clear"
Enter

Type "# Lane 1, head: print mode answers and exits 0"
Enter
Type "./ask -p hello; echo exit=$?"
Enter
Sleep 2s

Type "# Lane 2: 'echo hi' calls the echo tool; the reply is the tool result"
Enter
Type "./ask -p 'echo hi'; echo exit=$?"
Enter
Sleep 2s

Type "# Lane 3: assistant error goes to stderr, stdout empty, exit 1"
Enter
Type "./ask -p 'fail boom' 2>/tmp/review/err.txt; echo exit=$?; echo stderr=$(cat /tmp/review/err.txt)"
Enter
Sleep 2.5s

Type "# Lane 5: paced 200-word reply (2 tokens/s), then Ctrl+C"
Enter
Type `time ASK_FAUX_TPS=2 ./ask -p "$(cat long.txt)"; echo exit=$?`
Enter
Sleep 2s
Ctrl+C
Sleep 2.5s

Type "# Done: 0, 0, 1 and 130 as the plan expects"
Enter
Sleep 3s
