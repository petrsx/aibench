# Launch → command menu (/help) → walk the three tabs → /quit.
# Run via `make tui-gif` (TAPE=demo) — the wrapper provides the
# isolated environment; never run vhs against a real config by hand.
# Copy this file to record another operation (tapes are self-contained:
# vhs ignores Set/Output inside Source'd tapes, so no shared header).
Output bin/demo.gif

Set Shell "bash"
Set Width 1200
Set Height 720
Set FontSize 14
Set Padding 12

Hide
Type "./bin/aibench"
Enter
Wait+Screen /Ask something/
Show

Sleep 1.5s
Type "/"
Sleep 1s
Type "help"
Sleep 1s
Enter
Sleep 2.5s
Escape
Sleep 1s

# shift+tab cycles Chat → Inspector → Prompt → Chat
Shift+Tab
Sleep 1.5s
Shift+Tab
Sleep 1.5s
Shift+Tab
Sleep 1s

Type "/quit"
Sleep 1s
Hide
Enter
