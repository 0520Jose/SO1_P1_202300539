#!/bin/bash

IMG_LOW="alpine:latest"
IMG_HIGH="polinux/stress"

# Log file
LOG_FILE="$(dirname "$0")/execution.log"

# Pull images silently
docker pull $IMG_LOW > /dev/null 2>&1
docker pull $IMG_HIGH > /dev/null 2>&1

for i in {1..10}; do
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

# Log con timestamp
echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): Se crearon 10 contenedores nuevos." >> "$LOG_FILE"