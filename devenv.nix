{ pkgs, lib, config, ... }:

{
  # ── Go toolchain ──────────────────────────────────────────────────────
  # go.mod pins `go 1.25.0`; the rolling nixpkgs in devenv.yaml provides a
  # matching toolchain. languages.go.enable also wires GOPATH/GOROOT into
  # this shell only — nothing leaks to the global system.
  languages.go.enable = true;

  # ── Extra dev tooling ─────────────────────────────────────────────────
  # Everything the Makefile and local dev touch: make, the linter, the Go
  # language server (for editor/gopls), git (for `git describe` in the
  # build's version stamp), and sqlite for poking at the local state DB.
  packages = [
    pkgs.gnumake
    pkgs.golangci-lint
    pkgs.gopls
    pkgs.git
    pkgs.sqlite
  ];

  # Convenience commands — run `build`, `run`, `lint`, or `test` directly
  # once inside the environment. They just call the project Makefile.
  scripts.build.exec = "make build";
  scripts.run.exec = "make run";
  scripts.lint.exec = "make lint";
  scripts.test.exec = "make test";

  enterShell = ''
    echo "mailtagger dev env ready — $(go version)"
    echo "commands: build · run · lint · test   (see Makefile)"
  '';

  # Sanity check the toolchain when running `devenv test`.
  enterTest = ''
    go version | grep -q "go1.2" || { echo "unexpected go version"; exit 1; }
  '';
}
