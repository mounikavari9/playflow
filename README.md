# PlayFlow

## Run locally

Start PostgreSQL, Redis and the API with Docker Compose:

```sh
docker compose up -d --build --wait
curl -i localhost:8080/healthz
curl -i localhost:8080/readyz
```

Stop the stack with `docker compose down`. Your database volume is kept.

To change local settings, copy `.env.example` to `.env`. Set `API_PORT` if port 8080 is already in use.
