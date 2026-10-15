# Project progress and coordination

- Read `PROGRESS.md` before starting project work. It is the single current human-facing record of progress, decisions, pending work, blockers and development history.
- Check the actual branch, worktree and relevant source before acting; historical candidate evidence may describe a different checkout.
- Update the relevant task and append a dated development-log entry in `PROGRESS.md` after every meaningful implementation change, investigation, test campaign, decision, blocker, commit, integration or release event, before handing off or ending the task. Record source/candidate identity, actual evidence, limitations and the next action. Routine read commands do not need individual entries.
- Keep implementation status separate from verification and release acceptance. Do not count missing, skipped, mocked, interrupted or historical results as current passes. Never invent a commit, test result or completion percentage.
- Record new user decisions and work items with stable IDs. Preserve previous failures and append corrections instead of erasing history.
- Use `PROGRESS.md` for current status rather than creating another progress/checkpoint narrative. Preserve existing technical specifications, machine-readable contracts, qualification inputs and immutable evidence; those still require their normal consistency updates when implementation changes.
- During parallel work, have one coordinator integrate progress updates. Other agents report their changes and evidence to that coordinator to avoid competing edits to the tracker.
- Keep credentials, passphrases and private content out of the progress log. No automatic background monitor is implied; reconcile external changes when resuming work.
