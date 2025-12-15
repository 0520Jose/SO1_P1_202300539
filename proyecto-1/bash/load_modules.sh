#!/bin/bash

echo "Cargando modulos del Kernel - SO1"

# Directorio del script y del modulo kernel
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KERNEL_DIR="$SCRIPT_DIR/../modulo-kernel"

cd "$KERNEL_DIR" || exit 1

# Borrar modulos anteriores si estan cargados
echo "Limpiando modulos anteriores"
sudo rmmod continfo 2>/dev/null
sudo rmmod sysinfo 2>/dev/null

# Limpiar compilación anterior
echo "Limpiando compilación anterior"
make clean > /dev/null 2>&1

# Compilar modulos
echo "Compilando modulos"
if make; then
    echo "Compilacion existosa"
else
    echo "Error en compilación"
    exit 1
fi

# Verificar que los archivos .ko existen
if [ ! -f "sysinfo.ko" ] || [ ! -f "continfo.ko" ]; then
    echo "Error: Archivos .ko no encontrados"
    exit 1
fi

# Cargar modulo sysinfo
echo "Cargando modulo sysinfo"
if sudo insmod sysinfo.ko; then
    echo "Modulo sysinfo cargado"
else
    echo "Error cargando sysinfo"
    exit 1
fi

# Cargar modulo continfo
echo "Cargando modulo continfo"
if sudo insmod continfo.ko; then
    echo "Modulo continfo cargado"
else
    echo "Error cargando continfo"
    sudo rmmod sysinfo
    exit 1
fi

# Verificar que estan cargados
echo "Verificando modulos cargados:"
if lsmod | grep -q "sysinfo" && lsmod | grep -q "continfo"; then
    echo "Ambos modulos estan activos"
    lsmod | grep "info"
else
    echo "Error: Los modulos no estan activos"
    exit 1
fi

# Verificar archivos en /proc
echo "Verificando archivos en /proc:"
if [ -e "/proc/sysinfo_so1_202300539" ] && [ -e "/proc/continfo_so1_202300539" ]; then
    echo "Archivos /proc creados correctamente"
    ls -lh /proc/sysinfo_so1_202300539 /proc/continfo_so1_202300539
else
    echo "Error: Archivos /proc no encontrados"
    exit 1
fi

echo "Modulos cargados exitosamente"