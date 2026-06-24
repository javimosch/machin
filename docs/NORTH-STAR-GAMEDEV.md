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
| terminal real-time game | [machin-game-demo-snake](https://github.com/javimosch/machin-game-demo-snake) | `raw_mode`/`read_key` (v0.41.0) |
| 2D GUI (shapes+text) | [machin-game-demo-2048](https://github.com/javimosch/machin-game-demo-2048) | scalars + by-value `Color` |
| sprites / textures | [machin-game-demo-flappy](https://github.com/javimosch/machin-game-demo-flappy) | `f32` structs, struct-return; drove `float()` (v0.43.0) |
| audio | [machin-game-demo-simon](https://github.com/javimosch/machin-game-demo-simon) | FFI **opaque handles** (v0.44.0) |
| 3D + per-object rotation | [machin-game-demo-3d](https://github.com/javimosch/machin-game-demo-3d) | FFI **nested cstructs** (v0.45.0) → drove **native math** (v0.46.0); rlgl matrix stack |
| 2D procedural animation | [machin-game-demo-anim](https://github.com/javimosch/machin-game-demo-anim) | composition on native math |
| procedural mesh (immediate mode) | [machin-game-demo-terrain](https://github.com/javimosch/machin-game-demo-terrain) | rlgl `rlVertex3f` stream; flat shading in MFL |
| **static GPU mesh** | [machin-game-demo-planet](https://github.com/javimosch/machin-game-demo-planet) | **pointer/array FFI** (v0.47.0): raw memory + `*T` deref param → `UploadMesh`/`LoadModelFromMesh` |
| **infinite procedural world** | [machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk) | **`noise2`/`noise3`** (v0.49.0) → fbm terrain, chunk-streamed GPU meshes, fly camera, neon buildings, instanced flora, shader fog, **skeletal fauna**, **10 km draw distance** |

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

**Tier 2 — real GPU meshes (reached, v0.47.0).** Build a mesh once and upload it
to a GPU vertex buffer (`Mesh` + `UploadMesh` + `LoadModelFromMesh`/`DrawModel`),
instead of re-emitting every frame. This was the first hard gap — it needed
**pointer/array FFI** (raw C buffers, struct-by-pointer), now done: `alloc`/`poke_*`
raw memory (v0.47.0) plus **pointer-bearing `cstruct` fields + an inout `T*` param**
(v0.48.0), so a `Mesh` is a `cstruct` the C compiler lays out — no hard-coded
offsets ([machin-game-demo-planet](https://github.com/javimosch/machin-game-demo-planet)).
Instancing (`DrawMeshInstanced`, fields of objects) is **done — composition**:
the `Matrix` transform array is raw memory + a `ptr`, the instancing shader
composes, and `Material` is a partial cstruct. [machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk)
draws thousands of GPU-instanced plants in one call.

**Tier 3 — procedural worlds (started).**
- **Planet / terrain generation:** the **infinite chunk-streamed terrain** is here
  ([machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk)) —
  fbm over the native **`noise2`** (v0.49.0), GPU-mesh chunks loaded/unloaded
  around a fly camera, procedurally placed buildings (clustered in city
  districts), **GPU-instanced flora** in the scrubland between (one
  `DrawMeshInstanced` call for thousands of plants), **depth fog** via a
  post-process shader, a herd of **procedurally-animated skeletal fauna**, and a
  **10 km draw distance** (custom projection matrix via `rlSetMatrixProjection`
  + a coarse recentered terrain underlay — a first **level-of-detail** step).
  Still ahead: finer **LOD** (mesh decimation, not just one coarse ring),
  **biomes/erosion**, and denser ecosystems.
- **2D & 3D skeleton animation (procedural) — started, composition.** Bone
  hierarchies and **forward kinematics** are just the rlgl matrix stack
  (`rlPush`/`rlTranslatef`/`rlRotatef`/`rlPop`) nested per joint — the cyberpunk
  fauna are FK skeletons with a sine-driven diagonal gait, no skinned mesh, no new
  feature. Still ahead: **IK** (inverse kinematics) for foot-planting, animation
  blending, and skinned meshes for organic bodies — those want the vector/matrix
  layer and more `Mesh`/`Matrix` FFI.

**Tier 4 — simulation sandbox (aspirational, not set in stone).** A combat-sim in
the spirit of **ArmA / Armed Assault**: real **ballistics** (drag, gravity, wind,
penetration), rigid-body **physics**, large streamed worlds, many agents. This is
a stretch goal that would exercise machin as a *simulation* language as much as a
rendering one: deterministic fixed-step loops, spatial partitioning, and probably
a **physics library** via FFI (e.g. an ODE/Bullet-style C lib) rather than
hand-rolled. Listed as a direction, not a plan.

## The feature roadmap (gaps, in rough dependency order)

Each is a candidate to be *driven by a demo*, not built speculatively:

1. ~~**Pointer / array FFI**~~ — **done (v0.47.0–v0.48.0):** raw memory
   (`alloc`/`poke_*`/`peek_*`/`free`, pointers as `int`), the `*T` deref param,
   `ptr` pass-by-pointer, **pointer-bearing `cstruct` fields**, and an **inout
   `T*`** param (so a `Mesh` is a cstruct the C compiler lays out — no offsets).
   A typed C array (`Matrix*`, `Vector3*`, …) is just raw memory passed as `ptr`,
   so `DrawMeshInstanced` and friends need no further FFI — just a demo.
2. **A vector/matrix layer** — vec2/vec3/vec4 + mat4 ops. Could be a vendored MFL
   module first; promote hot paths to builtins if measured.
3. ~~**Shaders / uniforms**~~ — **reachable via the FFI (composition), demonstrated:**
   `Shader` is a pointer-field cstruct, `RenderTexture2D` is nested cstructs, and
   `LoadShaderFromMemory`/`SetShaderValue(..., ptr, kind)`/`SetShaderValueTexture`
   are plain FFI — no new language feature. [machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk)
   does a depth-fog post-process pass. The **same path** now unblocks real GPU
   instancing (an instancing VS + `DrawMeshInstanced`) and textured/lit materials —
   those are demos to build, not language gaps.
4. ~~**A noise builtin**~~ — **done (v0.49.0):** `noise2`/`noise3` (Perlin),
   deterministic, ~`[-1,1]`; fbm layered in MFL. The backbone of procedural worlds.
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
