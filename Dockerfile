FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV HOME=/root
ENV PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.cargo/bin:/usr/local/go/bin:$PATH"
ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

# Polling prompts and skills
COPY polling /opt/claudebox/polling
COPY skills /root/.claude/skills
COPY CLAUDE.md /root/CLAUDE.md

RUN mkdir -p /workspace /root/.claude/skills \
    && chmod +x /opt/claudebox/polling/poll-runner.sh

WORKDIR /workspace

# cbx binary — copy pre-built or download at runtime
COPY cbx /usr/local/bin/cbx
RUN chmod +x /usr/local/bin/cbx

EXPOSE 22 6080

CMD ["cbx", "setup"]
