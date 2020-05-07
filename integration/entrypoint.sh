#!/bin/bash
cd /etc && envsubst < /etc/purged-kafka.conf.tpl > /etc/purged-kafka.conf
/usr/bin/purged -backend_addr ${MY_IP}:3128 -frontend_addr ${MY_IP}:8080 -kafkaConfig /etc/purged-kafka.conf -topics resource_change -purgeMaxAge 100