# Daily Loki repair

`.github/workflows/daily-loki.yml` runs on GitHub-hosted Ubuntu daily at
16:23 UTC (09:23 PDT / 08:23 PST), with a manual **Run workflow** trigger.
Merge the workflow into the default branch to enable scheduling.

In **Settings → Secrets and variables → Actions**, add repository secrets:

- `LOKI_TOKEN`: Grafana Cloud access-policy token with `logs:read` for the Careme
  tenant. Use the value already stored in kage's `secrets/envtest`; do not upload
  your SSH private key or the entire decrypted secret file.
- `OPENAI_API_KEY`: OpenAI API key with API billing available for the Codex action.
  This job uses API credentials, not the desktop ChatGPT login.

Optional repository variables override the built-in connection defaults:
`LOKI_USER=1563943` and `LOKI_URL=https://logs-prod-021.grafana.net`.

Enable **Settings → Actions → General → Workflow permissions → Allow GitHub
Actions to create and approve pull requests**. The job requests contents and
pull-request write permissions; it creates draft PRs and never approves or merges.

The built-in `GITHUB_TOKEN` needs no additional secret. PRs created with this token
will not automatically trigger the existing push/pull_request CI workflows.
The daily job runs formatting checks, tests, vet, and lint before opening the PR.
If you need normal PR CI to trigger too, set optional secret `LOKI_REPAIR_PR_TOKEN`
to a fine-grained GitHub token restricted to this repository with Contents and
Pull requests read/write permissions. This token is used only for PR publication.

The job queries 26 hours of production errors in hourly batches, giving a two-hour
overlap for scheduling delays. GitHub scheduling is best effort; delays longer
than the overlap can leave gaps. Repeated diagnostic messages are grouped. The
job fails explicitly if an hourly batch hits Loki's 5,000-entry limit.
Only selected diagnostic fields enter the report; token and email redaction is
best effort, so logs should not contain secrets or personal data. Reports are
supplied to the OpenAI API, but raw reports are not uploaded as Actions artifacts.

An existing open `automation/daily-loki-repair` PR pauses further investigations
until reviewed. Otherwise Codex checks recent commits, attempts at most one
supported fix, and explains when evidence is insufficient. It receives a pinned
Grafana Loki skill and the report, without Loki credentials. Prompts prohibit
obeying instructions in logs, changing deployment/secrets/workflows, or treating
log suppression alone as a fix. Publication is restricted to `internal/` and
`cmd/`. The workflow times out after 30 minutes. Failed validation prevents a PR.

After adding secrets, use **Actions → Daily Loki repair → Run workflow** for the
first hosted test. Inspect the job summary and any draft PR. No live hosted run
is performed merely by adding these files locally.

Local validation:

```sh
python3 -B -m unittest discover -s .github/codex -p 'test_*.py'
actionlint .github/workflows/daily-loki.yml
```
