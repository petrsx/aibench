# Starter scripts: a scripted conversation replayed exactly, each prompt
# sent after the previous reply. Launch offers the selection — picking is
# always yours, nothing auto-sends.
# Run via `make tui-gif TAPE=starters`.
Output docs/media/starters.gif

Set Shell "bash"
Set Width 1240
Set Height 780
Set FontSize 13
Set Padding 10

Hide
Type "./bin/aibench"
Enter
Wait+Screen /Run starter script/
Show

Sleep 2s
Enter
Wait+Screen /drizzle/
Sleep 3s

Hide
Type "/quit"
Enter
