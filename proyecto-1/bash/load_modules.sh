#!/bin/bash

echo "========================================="
echo "Cargando Módulos del Kernel - SO1"
echo "========================================="

# Obtener directorio del script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KERNEL_DIR="$SCRIPT_DIR/../modulo-kernel"

cd "$KERNEL_DIR" || exit 1

# Descargar módulos anteriores si existen
echo "🧹 Limpiando módulos anteriores..."
sudo rmmod continfo 2>/dev/null
sudo rmmod sysinfo 2>/dev/null

# Limpiar compilación anterior
echo "🔨 Limpiando compilación anterior..."
make clean > /dev/null 2>&1

# Compilar módulos
echo "🔧 Compilando módulos..."
if make; then
    echo "✅ Compilación exitosa"
else
    echo "❌ Error en compilación"
    exit 1
fi

# Verificar que los archivos .ko existen
if [ ! -f "sysinfo.ko" ] || [ ! -f "continfo.ko" ]; then
    echo "❌ Error: Archivos .ko no encontrados"
    exit 1
fi

# Cargar módulo sysinfo
echo "📥 Cargando módulo sysinfo..."
if sudo insmod sysinfo.ko; then
    echo "✅ Módulo sysinfo cargado"
else
    echo "❌ Error cargando sysinfo"
    exit 1
fi

# Cargar módulo continfo
echo "📥 Cargando módulo continfo..."
if sudo insmod continfo.ko; then
    echo "✅ Módulo continfo cargado"
else
    echo "❌ Error cargando continfo"
    sudo rmmod sysinfo  # Limpiar el primero si el segundo falla
    exit 1
fi

# Verificar que están cargados
echo ""
echo "🔍 Verificando módulos cargados:"
if lsmod | grep -q "sysinfo" && lsmod | grep -q "continfo"; then
    echo "✅ Ambos módulos están activos"
    lsmod | grep "info"
else
    echo "❌ Error: Los módulos no están activos"
    exit 1
fi

# Verificar archivos en /proc
echo ""
echo "🔍 Verificando archivos en /proc:"
if [ -e "/proc/sysinfo_so1_202300539" ] && [ -e "/proc/continfo_so1_202300539" ]; then
    echo "✅ Archivos /proc creados correctamente"
    ls -lh /proc/sysinfo_so1_202300539 /proc/continfo_so1_202300539
else
    echo "❌ Error: Archivos /proc no encontrados"
    exit 1
fi

echo ""
echo "========================================="
echo "✅ Módulos cargados exitosamente"
echo "========================================="