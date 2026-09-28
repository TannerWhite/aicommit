Follow Conventional Commits (https://conventionalcommits.org/en/v1.0.0).

FORMAT:
  type(scope): short imperative description

  optional body - explain what and why, not how (wrap at 72 chars)

  optional footer (BREAKING CHANGE, Refs, Co-authored-by)

TYPES:
  feat      new feature
  fix       bug fix
  refactor  restructure, no behavior change
  perf      performance improvement
  test      add or update tests
  docs      documentation only
  style     formatting, no logic change
  build     build system or dependencies
  chore     tooling and maintenance
  ci        CI/CD changes
  revert    reverts a previous commit

SCOPE: a short noun for the affected module or area. Omit it when the
change is truly global.

DO:
  - lowercase imperative mood: "add", "fix", "remove"
  - keep the subject line under 72 characters, no trailing period
  - separate subject and body with a blank line
  - one logical change per commit
  - mark breaking changes with "!" after type/scope or a BREAKING CHANGE footer

DON'T:
  - vague subjects like "update files" or "fix stuff"
  - past tense ("fixed", "added")
  - ALL CAPS types
