#!/bin/bash
RC=1
SLEEP_INTERVAL=1
COUNTER=1
while [ $RC -ne 0 ]; do
    SLEEP=$(( SLEEP_INTERVAL*COUNTER ))
    echo "Waiting $SLEEP seconds"
    sleep $SLEEP
    echo "Sending messages to kafka (attempt $COUNTER)";
    kafkacat -b "${MY_IP}:19092" -t resource_change -P -l /data/messages.json
    RC=$?
    COUNTER=$(( COUNTER+1 ))
done
echo "Messages produced to the topic. You should see the purges shortly."
