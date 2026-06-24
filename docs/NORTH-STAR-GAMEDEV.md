# Game-dev: a north star for the domain

machin is a **backend-first** language — its core target is servers, tools, and
protocols. But the dogfood loop (build real things; let real use surface gaps)
keeps producing games, and **game development has emerged as one of machin's
primary detected domains**. This document is the north star for that domain: it
is *not* a promise or a committed roadmap — it records the direction so the next
demo knows which gap is worth driving.

> The overall machin north star is unchanged (backend, machine-first). Game-dev
> is a **branch** of it: a domain machin is being extended toward, one verified
> step at a time. Several aspirations below (the combat sim especially) are
> ideas, not commitments.

## Where it stands today (all shipping, all verified on-screen)

Native binaries through raylib's C FFI (and a terminal track):

| capability | repo | what it proved |
|---|---|---|
| terminal real-time game | [machin-game-snake](https://github.com/javimosch/machin-game-snake) | `raw_mode`/`read_key` (v0.41.0) |
| 2D GUI (shapes+text) | [machin-game-2048](https://github.com/javimosch/machin-game-2048) | scalars + by-value `Color` |
| sprites / textures | [machin-game-flappy](https://github.com/javimosch/machin-game-flappy) | `f32` structs, struct-return; drove `float()` (v0.43.0) |
| audio | [machin-game-simon](https://github.com/javimosch/machin-game-simon) | FFI **opaque handles** (v0.44.0) |
| 3D + per-object rotation | [machin-demo-3d](https://github.com/javimosch/machin-demo-3d) | FFI **nested cstructs** (v0.45.0) → drove **native math** (v0.46.0); rlgl matrix stack |
| 2D procedural animation | [machin-demo-anim](https://github.com/javimosch/machin-demo-anim) | composition on native math |
| procedural mesh (immediate mode) | [machin-demo-terrain](https://github.com/javimosch/machin-demo-terrain) | rlgl `rlVertex3f` stream; flat shading in MFL |

The how-to substrate is [`skills/machin-gamedev/SKILL.md`](../skills/machin-gamedev/SKILL.md):
setup, the FFI surface, the **headerless-extern trick** (reach any scalar/`void`
`libraylib.a` symbol without its header), and the int/float rules.

So today machin can do: real 2D games (input/sprites/audio), animated 3D scenes
with cameras and per-object transforms, procedural 2D animation, and procedural
meshes streamed each frame. That is already a real game-dev surface.

## The vision, in tiers (near → far)

**Tier 1 — solid interactive 3D (mostly here).** Cameras, primitives, per-object
transforms, immediate-mode procedural geometry, native math. Missing for comfort:
a small **vector/matrix helper layer** (vec3/mat4 ops as MFL or builtins) and
**texture-mapped** meshes.

**Tier 2 — real GPU meshes.** Build a mesh once and upload it to a GPU vertex
buffer (`Mesh` + `UploadMesh`/`DrawMesh`), instead of re-emitting every frame.
This is the first hard gap: it needs **pointer/array FFI** (raw C `float*`
buffers, struct-by-pointer). Unlocks large **static** terrain, models, and
`DrawMeshInstanced` for fields of objects.

**Tier 3 — procedural worlds.**
- **Planet / terrain generation:** chunked height fields with level-of-detail,
  biomes, erosion; **scattering** of *flora/fauna* (instanced meshes placed by
  density functions and noise). Needs Tier-2 meshes + instancing + a real noise
  primitive (Perlin/Simplex — a candidate native builtin, since layered `sin` only
  goes so far).
- **2D & 3D skeleton animation (procedural):** bone hierarchies, forward
  kinematics, and **IK** (inverse kinematics) for limbs; blending. Mostly MFL math
  over transforms — but benefits from the vector/matrix layer and, for 3D, skinned
  meshes (more `Mesh`/`Matrix` FFI).

**Tier 4 — simulation sandbox (aspirational, not set in stone).** A combat-sim in
the spirit of **ArmA / Armed Assault**: real **ballistics** (drag, gravity, wind,
penetration), rigid-body **physics**, large streamed worlds, many agents. This is
a stretch goal that would exercise machin as a *simulation* language as much as a
rendering one: deterministic fixed-step loops, spatial partitioning, and probably
a **physics library** via FFI (e.g. an ODE/Bullet-style C lib) rather than
hand-rolled. Listed as a direction, not a plan.

## The feature roadmap (gaps, in rough dependency order)

Each is a candidate to be *driven by a demo*, not built speculatively:

1. **Pointer / array FFI** — pass raw C buffers (`float*`, `unsigned short*`) and
   structs **by pointer** (`UploadMesh(Mesh*)`). The single biggest unlock
   (Tier 2+). Probably a `cbuffer`/`carray` concept + `&struct` marshaling.
2. **A vector/matrix layer** — vec2/vec3/vec4 + mat4 ops. Could be a vendored MFL
   module first; promote hot paths to builtins if measured.
3. **Shaders / uniforms** — `LoadShader`, `SetShaderValue` (needs pointer FFI for
   uniform arrays); lighting, post-processing.
4. **A noise builtin** — Perlin/Simplex; the backbone of procedural worlds.
5. **FFI callbacks (Phase 4)** — C calling back into MFL (custom render/audio
   callbacks, `SetTraceLogCallback`, GLFW-style input hooks).
6. **Deterministic fixed-step sim** loop patterns + spatial structures (for Tier 4).

## Method

Same loop as the rest of machin: build a real thing, hit the wall, fill it in the
language, release, and record it — in the [dogfood table](../skills/machin-gamedev/SKILL.md#each-game-drove-a-feature-the-dogfood-record).
Be honest about **composition vs. new feature**: per-object rotation, 2D
procedural animation, and immediate-mode meshes were all *composition* on the
existing FFI — that the language already reaches them is itself the result. The
next demo that genuinely *can't* be expressed (real GPU meshes) names the next
feature (pointer/array FFI).

To contribute a game/demo: build something real, put it in its own public repo
with a `build.sh`, link the gamedev skill, and add it to
[awesome-machin](https://github.com/javimosch/awesome-machin).
