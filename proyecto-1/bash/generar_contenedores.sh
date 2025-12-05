#!/bin/bash

IMG_LOW="so1_low"
IMG_RAM="so1_high_ram"
IMG_CPU="so1_high_cpu"

for i in {1..10}; do
    RANDOM_TYPE=$((1 + $RANDOM % 3))

    CONTAINER_NAME="so1_contenedor_$RANDOM"

    case $RANDOM_TYPE in
        1)
            docker run -d --name "$CONTAINER_NAME" "$IMG_LOW" > /dev/null 2>&1
            ;;
        2)
            docker run -d --name "$CONTAINER_NAME" "$IMG_RAM" > /dev/null 2>&1
            ;;
        3)
            docker run -d --name "$CONTAINER_NAME" "$IMG_CPU" > /dev/null 2>&1
            ;;
    esac
done

echo "$(date): Se crearon 10 contenedores nuevos." >> ~/proyecto-1/bash/execution.log
