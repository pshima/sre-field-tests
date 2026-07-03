# The operator container is the agent's on-call shell: the Docker CLI (against
# the mounted host socket) plus common tools, and — for this DB scenario — the
# postgres client so the agent can inspect pg_stat_activity.
FROM docker:27-cli
RUN apk add --no-cache bash curl procps coreutils jq less grep postgresql-client
WORKDIR /root
