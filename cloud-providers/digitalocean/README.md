# DigitalOcean Provider

## Secrets Required

| Secret | Description |
|--------|-------------|
| `DIGITALOCEAN_ACCESS_TOKEN` | DigitalOcean API token |
| `SSH_PUBLIC_KEY` | Your public SSH key |

## Deploy

1. Create an API token from [DigitalOcean API settings](https://cloud.digitalocean.com/account/api/tokens)
2. Add the secrets above to your GitHub repo (Settings → Secrets → Actions)
3. Go to Actions → **"Deploy to DigitalOcean"** → Run workflow
4. SSH in: `ssh root@<droplet-ip>`

## Manual Deploy

```bash
# Install doctl: brew install doctl (macOS) or snap install doctl (Linux)
doctl auth init

doctl compute droplet create claude-devbox \
  --image docker-20-04 \
  --size s-4vcpu-8gb \
  --region nyc1 \
  --ssh-keys <your-ssh-key-fingerprint>

ssh root@<droplet-ip>
git clone https://github.com/your/claude-devbox.git /opt/claude-devbox
cd /opt/claude-devbox
docker build -t claude-devbox .
docker run -d --name claude-devbox --restart unless-stopped \
  -p 22:22 -e SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)" claude-devbox
```
