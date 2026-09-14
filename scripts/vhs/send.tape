# Two sends: ask, watch the reply stream, then follow up — the record
# lines and the panels fill in behind the conversation. The README's hero.
# Run via `make tui-gif TAPE=send` — the wrapper supplies the demo
# config and the stand-in endpoint; never run vhs against a real config.
Output docs/media/send.gif

Set Shell "bash"
Set Width 1240
Set Height 780
Set FontSize 13
Set Padding 10

Hide
Type "./bin/aibench"
Enter
# The demo prompt set declares starters, so launch opens their selection;
# esc backs out of it (nothing is ever sent without picking).
Wait+Screen /Run starter script/
Escape
Show

Sleep 1.2s
Type "What's the weather in Paris, France?"
Sleep 600ms
Enter
Wait+Screen /humidity/
Sleep 2s

# A follow-up: the second record joins the same conversation, and the
# panels re-measure it — which is the point of the tab.
Type "And is that good weather for a run?"
Sleep 600ms
Enter
Wait+Screen /drizzle/
Sleep 3s

Hide
Type "/quit"
Enter
