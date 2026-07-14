{
  description = "emerbot development environment";

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
        pkgs = import nixpkgs {
          inherit system;
          config = {
            allowUnfree = true;
          };
        };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            awscli2
            bashInteractive
            direnv
            go
            gofumpt
            golangci-lint
            gopls
            jq
            opencode
            claude-code
            opentofu
            zip
          ];

          env = {
            CGO_ENABLED = "0";
            GO111MODULE = "on";
          };

          shellHook = ''
            export PATH="$PWD/bin:$PATH"
            echo "emerbot dev shell carregado"
          '';
        };

        formatter = pkgs.nixpkgs-fmt;
      }
    );
}
