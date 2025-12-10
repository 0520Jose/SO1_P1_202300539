#!/bin/bash

IMG_LOW="alpine:latest"
IMG_HIGH="polinux/stress"

# Archivo de guardado de logs
LOG_FILE="$(dirname "$0")/execution.log"

# Pull de imagenes necesarias
docker pull $IMG_LOW > /dev/null 2>&1
docker pull $IMG_HIGH > /dev/null 2>&1

# Contar contenedores actuales
CURRENT_COUNT=$(docker ps --filter "name=so1_contenedor" --format "{{.ID}}" | wc -l)

echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): Contenedores actuales: $CURRENT_COUNT" >> "$LOG_FILE"

# Comprobacion de limite de contenedores
if [ "$CURRENT_COUNT" -ge 10 ]; then
    echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): LÍMITE ($CURRENT_COUNT contenedores)" >> "$LOG_FILE"
    exit 0
fi

# Calculo contenedores a crear
TO_CREATE=$((10 - CURRENT_COUNT))

echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): Creando $TO_CREATE contenedores nuevos..." >> "$LOG_FILE"

# Creación de contenedores
for i in $(seq 1 $TO_CREATE); do
    RANDOM_TYPE=$((1 + $RANDOM % 3))
    CONTAINER_NAME="so1_contenedor_$RANDOM"

    case $RANDOM_TYPE in
        1)
            # Bajo consumo
            docker run -d --name "$CONTAINER_NAME" "$IMG_LOW" sleep infinity > /dev/null 2>&1
            ;;
        2)
            # Alto consumo RAM
            docker run -d --name "$CONTAINER_NAME" "$IMG_HIGH" stress --vm 1 --vm-bytes 128M > /dev/null 2>&1
            ;;
        3)
            # Alto consumo CPU
            docker run -d --name "$CONTAINER_NAME" "$IMG_HIGH" stress --cpu 1 > /dev/null 2>&1
            ;;
    esac
done

# Cantidad de contenedores final
FINAL_COUNT=$(docker ps --filter "name=so1_contenedor" --format "{{.ID}}" | wc -l)
echo "$(date '+%a %d %b %Y %H:%M:%S %Z') Total actual: $FINAL_COUNT contenedores." >> "$LOG_FILE"