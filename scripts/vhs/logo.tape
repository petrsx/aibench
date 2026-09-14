# The wordmark, drawn by the app itself at a large font so the README can
# use it as a heading — same glyphs, same theme color, no hand-drawn copy
# to drift from theme.LogoArt.
#
# Run via `make tui-gif TAPE=logo`, then cut the wordmark out of the
# recording (vhs 0.11's Screenshot writes nothing here, so the frame comes
# from the gif):
#
#   ffmpeg -sseof -0.5 -i bin/logo.gif -frames:v 1 -update 1 -y frame.png
#   ffmpeg -i frame.png -vf "crop=280:96:18:16" -y docs/media/logo.png
#
# The gif itself is a byproduct; one frame of it is the asset.
Output bin/logo.gif

Set Shell "bash"
Set Width 1600
Set Height 420
Set FontSize 32
Set Padding 20

Hide
Type "./bin/aibench"
Enter
Wait+Screen /Chat/
# vhs writes no gif at all if every frame is hidden.
Show
Sleep 1s
Hide
Type "/quit"
Enter
