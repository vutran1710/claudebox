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
4. From your laptop, set it up and start the always-on session:
   `cbx-setuptool setup --host root@<droplet-ip> --with-api`, then `ssh root@<droplet-ip> cbx new master`

## Manual Deploy

```bash
# Install doctl: brew install doctl (macOS) or snap install doctl (Linux)
doctl auth init

doctl compute droplet create claudebox \
  --image ubuntu-24-04-x64 \
  --size s-4vcpu-8gb \
  --region nyc1 \
  --ssh-keys <your-ssh-key-fingerprint>

# then, from your laptop, as for any machine
cbx-setuptool setup --host root@<droplet-ip> --with-api
ssh root@<droplet-ip> cbx new master
```
