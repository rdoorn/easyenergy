#!/bin/bash

case $1 in
    logs)
        docker logs %NAME%
        ;;
    start)
        docker run --name=%NAME% \
        --net=domoticanet \
        --restart=unless-stopped \
        -d \
        -e TZ=Europe/Amsterdam \
        -e STATSD_URL="telegraf:8125" \
        -e MQTT_URL="mqtt://mosquitto:1883/" \
        --log-opt max-size=10m \
        --log-opt max-file=3  \
        %NAME%
        ;;
    stop)
        docker stop %NAME% | xargs docker rm
        ;;
    *)
        echo "unknown or missing parameter $1"
esac
