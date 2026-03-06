# Mattermost Google Meet Plugin

Create Google Meet links directly from Mattermost.

## Features

- Create a Google Meet from the channel header button.
- Authenticate each user with Google OAuth.
- Post the generated Meet link back into the channel.

## Configuration

In Mattermost, go to **System Console > Plugin Management > Google Meet** and configure:

- `Google Client ID`
- `Google Client Secret`
- `Encryption Key`

The OAuth callback URL to register in Google Cloud is:

```text
https://YOUR_MATTERMOST_SITE_URL/plugins/com.mattermost.google-meet/oauth2/callback
```

## Development

Requirements:

- Go
- Node.js and npm
- A Mattermost server with plugin uploads enabled

Build the plugin:

```bash
make apply dist
```

The bundle will be created at:

```text
dist/com.mattermost.google-meet-0.1.2.tar.gz
```

Deploy to a Mattermost server:

```bash
export MM_SERVICESETTINGS_SITEURL=http://localhost:8065
export MM_ADMIN_TOKEN=your_token_here
make deploy
```

## Release

1. Update code and metadata.
2. Commit changes.
3. Create a version tag.
4. Build the bundle with `make apply dist`.
5. Create a GitHub release and attach the `.tar.gz` bundle.

## Repository

- Homepage: https://github.com/reston/mattermost-plugin-google-meet
- Issues: https://github.com/reston/mattermost-plugin-google-meet/issues
