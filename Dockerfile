
FROM docker-registry.wikimedia.org/wikimedia-buster:latest
COPY ./purged /usr/bin/purged
COPY ./integration/kafka.conf /etc/purged-kafka.conf.tpl
COPY ./integration/entrypoint.sh /bin/entrypoint
RUN apt-get update && apt-get install -y --no-install-recommends gettext-base librdkafka1 && apt-get clean && rm -rf /var/lib/apt/lists/*
CMD [ "/bin/bash", "-c", "/bin/entrypoint" ]