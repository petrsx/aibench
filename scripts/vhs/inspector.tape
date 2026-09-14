# What actually went on the wire: send once, then cross to the Inspector
# and flip request → response → decoded body.
# Run via `make tui-gif TAPE=inspector`.
Output docs/media/inspector.gif

Set Shell "bash"
Set Width 1240
Set Height 780
Set FontSize 13
Set Padding 10

Hide
Type "./bin/aibench"
Enter
Wait+Screen /Run starter script/
Escape
Type "What's the weather in Paris, France?"
Enter
Wait+Screen /humidity/
Show

Sleep 1.5s
# shift+tab: Chat → Inspector, opening on what was sent
Shift+Tab
Sleep 2.5s
# tab: the response side — status, headers, the SSE body as received
Tab
Sleep 2.5s
# ctrl+r: the same body decoded rather than as the wire carried it
Ctrl+r
Sleep 2.5s

Hide
Shift+Tab
Shift+Tab
Type "/quit"
Enter
