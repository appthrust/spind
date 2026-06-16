{
  description = "spind image builder";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          image-builder = pkgs.mkShell {
            packages = [
              pkgs.bash
              pkgs.coreutils
              pkgs.e2fsprogs
              pkgs.git
              pkgs.go
              pkgs.jq
            ];
          };
        });
    };
}
