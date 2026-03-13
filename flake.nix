{
  description = "packwiz-tui — a pretty TUI wrapper around the packwiz CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        packwiz-tui = pkgs.buildGoModule {
          pname = "packwiz-tui";
          version = "0.1.0";
          src = ./.;

          # Run `nix build` with pkgs.lib.fakeHash first, then paste the
          # "got:" hash printed in the error here.
          vendorHash = pkgs.lib.fakeHash;

          nativeBuildInputs = [
            pkgs.makeWrapper
            pkgs.lazygit
          ];

          postInstall = ''
            wrapProgram $out/bin/packwiz-tui \
              --suffix PATH : ${
                pkgs.lib.makeBinPath [
                  pkgs.git
                  pkgs.packwiz
                ]
              }
          '';

          meta = with pkgs.lib; {
            description = "Terminal UI wrapper for packwiz Minecraft modpack management";
            license = licenses.mit;
            mainProgram = "packwiz-tui";
            platforms = platforms.unix;
          };
        };

      in
      {
        packages.default = packwiz-tui;
        packages.packwiz-tui = packwiz-tui;

        apps.default = flake-utils.lib.mkApp {
          drv = packwiz-tui;
          name = "packwiz-tui";
        };

        devShells.default = pkgs.mkShell {
          name = "packwiz-tui-dev";
          packages = with pkgs; [
            go
            gopls
            gotools
            golangci-lint
            delve
            git
            packwiz
            lazygit
          ];
          shellHook = ''
            echo ""
            echo "  packwiz-tui dev shell — $(go version | awk '{print $3}')"
            echo "  go run .   run from source"
            echo "  nix build  build package"
            echo ""
          '';
        };
      }
    );
}
