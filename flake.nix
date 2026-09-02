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
    {
      # Overlay for easy integration into NixOS/home-manager
      overlays.default = final: prev: {
        packwiz-tui = self.packages.${final.system}.default;
      };
    }
    // flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        packwiz-tui = pkgs.buildGoModule {
          pname = "packwiz-tui";
          version = "0.1.0";
          src = ./.;

          proxyVendor = true;
          vendorHash = "sha256-lMZVjSQeGOR3Z+q7r8bNExo9p0H0ql2OwiXpuekgLlY=";

          nativeBuildInputs = [ pkgs.makeWrapper ];

          postInstall = ''
            wrapProgram $out/bin/packwiz-tui \
              --suffix PATH : ${
                pkgs.lib.makeBinPath [
                  pkgs.git
                  pkgs.packwiz
                  pkgs.lazygit
                  # test harness: server boot + headless client soak
                  pkgs.temurin-bin-21
                  pkgs.portablemc
                  pkgs.gamescope # also provides gamescopectl for screenshots
                ]
              }
          '';

          meta = with pkgs.lib; {
            description = "Terminal UI wrapper for packwiz Minecraft modpack management";
            homepage = "https://github.com/flashgnash/packwiz-tui";
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
            temurin-bin-21
            portablemc
            gamescope
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
