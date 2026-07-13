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
            opentofu
            zip
          ];

          env = {
            CGO_ENABLED = "0";
            GO111MODULE = "on";
          };

          shellHook = ''
            if aws sts get-caller-identity >/dev/null 2>&1; then
              eval "$(aws configure export-credentials --format env)"
            fi
            export PATH="$PWD/bin:$PATH"
            echo "emerbot dev shell carregado"
          '';
        };

        formatter = pkgs.nixpkgs-fmt;
      }
    );
}
