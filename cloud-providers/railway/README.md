# Railway Provider

## Secrets Required

| Secret | Description |
|--------|-------------|
| `RAILWAY_TOKEN` | Railway project token |
| `SSH_PUBLIC_KEY` | Your public SSH key |

## Deploy

1. Create a Railway project from the [Railway dashboard](https://railway.com/dashboard)
2. Add the secrets above to your GitHub repo (Settings → Secrets → Actions)
3. Go to Actions → **"Deploy to Railway"** → Run workflow
4. In Railway, expose port 22 (TCP) in networking settings
5. SSH in: `ssh root@<railway-host> -p <port>`

## Manual Deploy

```bash
curl -fsSL https://railway.com/install.sh | sh
railway link
railway variables set SSH_PUBLIC_KEY="$(cat ~/.ssh/id_ed25519.pub)"
railway up --detach
```

Then expose port 22 in Railway networking settings and SSH in.
