# The operator container is the agent's on-call shell. It has the Docker CLI
# (talking to the mounted host socket) plus the everyday tools an SRE reaches
# for when triaging a container incident.
FROM docker:27-cli
RUN apk add --no-cache bash curl procps coreutils jq less grep
WORKDIR /root
