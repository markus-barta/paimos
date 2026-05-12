{ pkgs, lib, ... }:

{
  packages = with pkgs; [
    cosign
    go
    gotools
    gopls
    shellcheck
  ];

  languages.javascript = {
    enable = true;
    npm = {
      enable = true;
      install.enable = true;
    };
  };

  env = {
    DATA_DIR = "./data";
    STATIC_DIR = "./frontend/dist";
  };

  enterShell = ''
    echo "PAIMOS dev environment"
    echo "  backend:  cd backend && go run ."
    echo "  frontend: cd frontend && npm run dev"
  '';
}
