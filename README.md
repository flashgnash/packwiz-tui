# Packwiz TUI

Vibe coded TUI wrapper
Works reasonably well, looks pretty, partly to test out claude code (cost around $20 in credits so far, expensive but to be fair it's made a pretty good tool)

Made to solve a problem, I've got a lot of nix workflow setup to easily deploy packwiz servers
A friend of mine is co admin of this server and needs to be able to work with it, so built this for a more user friendly means of doing that
(also, the reason for it being a TUI is so he can do so on the server itsself over SSH and not have to install anything locally)

Video demonstration
https://minecraft.flashgnash.co.uk/uploads/26-03-14-23:27:44-kitty.mp4

The main page is the mod list
Search with /, navigate with arrow keys or vim motions, enter to edit the selected file manually with $EDITOR, D to delete or restore mod
G is a shortcut to lazygit as I don't want to automatically create git commits, plus why reinvent the wheel

Requirements:
- Packwiz
- Lazygit
- A text editor
