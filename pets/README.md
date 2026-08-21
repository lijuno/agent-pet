# Where pets live

Two locations, for two different reasons.

**Built-in packs** are in `ui/dist/pets/` and are embedded into the binary at
build time. They are served by the Wails asset server at `/pets/<id>/<file>`, so
a fresh install has characters with nothing to install and nothing to lose.
Regenerate them with `make pets`.

**Your packs** go in `~/.local/share/agent-pet/pets/<id>/`. They are read from
disk at startup and served at `/userpets/<id>/<file>`. A pack whose `id` matches
a built-in replaces it, which is how you override a shipped character with your
own version.

This directory holds no assets — it exists so the split is documented where you
would look for it. The format is in [`../docs/pets.md`](../docs/pets.md), and
the reasoning is in [ADR 0003](../docs/adr/0003-pet-asset-format.md).
