# The operator container is the agent's on-call shell: the Docker CLI (against
# the mounted host socket) plus the everyday tools an SRE reaches for.
FROM docker:27-cli
RUN apk add --no-cache bash curl procps coreutils jq less grep
WORKDIR /root
