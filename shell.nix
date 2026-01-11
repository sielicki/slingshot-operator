{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
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
    if [ -n "$KUBEBUILDER_ASSETS" ]; then
      echo "KUBEBUILDER_ASSETS=$KUBEBUILDER_ASSETS"
    fi
  '';
}
