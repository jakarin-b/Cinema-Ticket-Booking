# Secret rotation

No real credential belongs in Git. Keep `.env` local and use a secret manager in deployed environments.

## Firebase service account

1. Create a second Firebase/Google service-account key.
2. Update `FIREBASE_CLIENT_EMAIL` and `FIREBASE_PRIVATE_KEY` in the deployment secret store.
3. Recreate API containers and verify Firebase login.
4. Disable and delete the previous key.

## Google OAuth client secret

Google OAuth clients may support overlapping secrets depending on console capabilities. Introduce the new secret, recreate API containers, validate the callback, and then revoke the old secret. The client ID and authorized redirect URIs should remain stable.

## Redis and RabbitMQ

Rotate local passwords by stopping services, changing `.env`, and recreating their volumes. In production, introduce a second user/password where supported, roll applications, then remove the old credential. Existing direct OAuth sessions are intentionally invalidated by Redis credential/storage replacement.

## SMTP credentials

Mailpit requires no local credentials. For a deployed SMTP provider, create a replacement SMTP user or app password, update `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_HOST`, `SMTP_PORT`, and `SMTP_FROM` in the secret/configuration store, recreate the worker, and confirm a booking email is accepted before revoking the previous credential. Use a STARTTLS endpoint; implicit TLS on port 465 is not supported by this client.

## Incident response

If any session, provider, or SMTP credential leaks, rotate the credential, clear Redis sessions and OAuth state when applicable, inspect `SYSTEM_ERROR`, worker, and request logs, and revoke affected provider sessions outside this application.
