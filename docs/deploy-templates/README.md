# Deployment templates

**Templates, not tested on the platform.** They are sketches with placeholder names to adapt, not deployable files. Nothing here has been run against Fly.io, Render or Railway, and the repository does not deploy anything. Read `docs/deploy.md` first. Secrets (`DATABASE_URL`, `LLM_API_KEY`, `LLM_MODEL`, `LLM_BASE_URL`) are set through each platform's secret store, never in these files.

- `fly.toml.template`
- `render.yaml.template`
- `railway.toml.template` (Railway; used by `docs/deploy-railway-supabase-vercel.md`)
- `.env.production.example` (placeholders for the Railway, Vercel and migration variables)
