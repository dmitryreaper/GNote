{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  buildInputs = with pkgs; [
    go
    gcc
    pkg-config
    mesa # OpenGL библиотеки
    libglvnd 
    xorg.libXcursor # Зависимость GLFW
    xorg.libXi
    xorg.libXrandr
    xorg.libXinerama
    xorg.libXxf86vm
    xorg.libXfixes
  ];
}