{
  description = "Slingshot Operator development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            go-tools
            golangci-lint
            kubebuilder
            kubectl
            kubernetes-helm
            kind
            docker
            kustomize
            kubernetes-controller-tools
            yq-go
            setup-envtest
          ];

          shellHook = ''
            export GOPATH="$HOME/go"
            export PATH="$GOPATH/bin:$PATH"

            # Set up KUBEBUILDER_ASSETS for envtest
            if [ -z "$KUBEBUILDER_ASSETS" ]; then
              ENVTEST_ASSETS=$(setup-envtest use -p path 2>/dev/null || true)
              if [ -n "$ENVTEST_ASSETS" ]; then
                export KUBEBUILDER_ASSETS="$ENVTEST_ASSETS"
              fi
            fi

            echo "Slingshot Operator development environment"
            echo "Go version: $(go version)"
            echo "Kubebuilder version: $(kubebuilder version 2>/dev/null || echo 'not available')"
            if [ -n "$KUBEBUILDER_ASSETS" ]; then
              echo "KUBEBUILDER_ASSETS=$KUBEBUILDER_ASSETS"
            fi
          '';
        };

        packages.default = pkgs.buildGoModule {
          pname = "slingshot-operator";
          version = "0.1.0";
          src = ./.;
          vendorHash = null; # Update after go mod tidy
        };
      });
}
