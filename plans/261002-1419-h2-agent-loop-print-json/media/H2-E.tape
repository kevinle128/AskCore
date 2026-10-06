Output H2-E-review.mp4

Set Shell "bash"
Set FontSize 16
Set Width 1400
Set Height 800
Set TypingSpeed 20ms
Set Padding 20

Hide
Type `cd /tmp/review/e && export PS1='$ ' && clear`
Enter
Show

Type "# H2-E review. Lane 1: trunk (H2-D) rejects JSON mode"
Enter
Type `./ask-trunk --mode json hi | jq -c .type; echo "exit=${PIPESTATUS[0]}"`
Enter
Sleep 2s

Type "# Lane 1, head: one JSON line per event, agent_start first, agent_settled last"
Enter
Type `./ask --mode json hi | jq -c '{seq,type}'; echo "exit=${PIPESTATUS[0]}"`
Enter
Sleep 3.5s
Type "clear"
Enter

Type "# Lane 3: assistant error still exits 0 in JSON mode (as Pi); last line is agent_settled"
Enter
Type `./ask --mode json 'fail boom' | jq -c 'select(.type=="message_end").message | {role,stopReason,errorMessage}'`
Enter
Sleep 2.5s
Type `./ask --mode json 'fail boom' | tail -1 | jq -c '{seq,type}'; echo "exit=${PIPESTATUS[0]}"`
Enter
Sleep 2.5s

Type "# Lane 6: 300 KB reply, reader closes the pipe after one line -> exit 1, not 141 (SIGPIPE)"
Enter
Type `./ask --mode json "$(head -c 300000 /dev/zero | tr '\0' a)" | head -n1 | cut -c1-70; echo "exit=${PIPESTATUS[0]}"`
Enter
Sleep 2.5s
Type `./ask-trunk -p "$(head -c 300000 /dev/zero | tr '\0' a)" | head -c1; s=${PIPESTATUS[0]}; echo; echo "trunk text mode exit=$s"`
Enter
Sleep 2s
Type `./ask -p "$(head -c 300000 /dev/zero | tr '\0' a)" | head -c1; s=${PIPESTATUS[0]}; echo; echo "head text mode exit=$s"`
Enter
Sleep 2s

Type "# Lane 7: slow reader (sleeps 3 s) stalls the run, no line lost"
Enter
Type `time (ASK_FAUX_TPS=50 ./ask --mode json "$(cat long.txt)" | (sleep 3; cat) | wc -l)`
Enter
Sleep 6s
Type `ASK_FAUX_TPS=50 ./ask --mode json "$(cat long.txt)" | wc -l`
Enter
Sleep 5.5s

Type "# Done: lanes 1, 3, 6 and 7 pass"
Enter
Sleep 3s
