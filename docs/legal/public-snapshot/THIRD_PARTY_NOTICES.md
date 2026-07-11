# TermX Third-Party Notices

TermX incorporates third-party software and font assets under their respective licenses.

- The `termx` binary embeds its exact Go dependency notices. Run `termx licenses` to print them.
- App distributions include `THIRD_PARTY_NOTICES.txt` and the complete files under `third-party/` in their static assets.
- Vendored source keeps upstream license files next to the corresponding material, including `termx-vterm/internal/vt/LICENSE`; App font terms are distributed in `termx-app/public/third-party/FONTS.txt`.

Release packages must regenerate notices from the committed lockfiles and resolved build graph with `scripts/license-audit.sh`. A source-level SPDX summary does not replace the complete copyright, license, and NOTICE texts required by an artifact's actual dependencies.
