FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV HOME=/root
ENV PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.cargo/bin:/usr/local/go/bin:$PATH"
ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

# Skills and config
COPY skills /root/.claude/skills
COPY CLAUDE.md /root/CLAUDE.md

RUN mkdir -p /root/workspace /root/.claude/skills

WORKDIR /root/workspace

# cbx binary — copy pre-built or download at runtime
COPY cbx /usr/local/bin/cbx
RUN chmod +x /usr/local/bin/cbx

EXPOSE 8091

CMD ["cbx", "serve", "--addr", "0.0.0.0:8091"]
