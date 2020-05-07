#!/bin/bash
set -e
function alert {
    printf "⚠️ \033[0;31m%s\033[0m\n" "$@"
}

function highlight {
    printf "👉 \033[1;34m%s\033[0m\n" "$@"
}

if [ -z "$MY_IP" ]; then
    alert "ERROR: You need to define a variable named MY_IP containing the IP address of your machine."
    exit 2
fi
if ! which docker-compose > /dev/null; then
    alert "ERROR: You need to install docker-compose in order to run the integration tests."
    exit 3
fi
if ! docker images > /dev/null 2>&1; then
    alert "ERROR: The current user does not have the permissions to run docker."
    alert "       Either run as root or add your user to the docker group."
    exit 3
fi
highlight "Generating messages to produce"
# We need messages to be recent so that they will be consumed.
DATESUBST=$(date +%Y-%m-%dT%TZ -u)
sed s/DATESUBST/$DATESUBST/ integration/producer/messages.json.tpl > integration/producer/messages.json
cat integration/producer/messages.json
highlight "Bringing up the cluster..."
docker-compose up -d
highlight "Waiting for the messages to be produced"
while true;
    do
    if docker-compose ps producer | grep -Fq 'Exit '; then
        docker-compose logs producer
        break
    fi
    sleep 2
    echo -n "."
done
highlight "Purges you sent should now show up in the web logs below. You can find the messages sent under integration/producer/messages.json"
sleep 1
docker-compose logs web
highlight "Here are the corresponding prometheus metrics"
curl -sL http://${MY_IP}:2112/metrics | grep purged
highlight "Stopping the cluster..."
docker-compose stop
highlight "Logs from purged:"
docker-compose logs purged
highlight "Removing the cluster..."
docker-compose down
echo -e "\033[0;32m ✔️ Test completed.\033[0m"
