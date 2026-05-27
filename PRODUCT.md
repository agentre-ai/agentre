# Product

## Register

product

## Users

Developers who already use multiple AI coding agents — Claude Code, Codex, and built-in engines — and have outgrown the single-terminal workflow. They are comfortable in IDEs and terminals, prefer keyboard-driven UX, run long multi-turn tasks, and care about local-first / sovereign tooling (no cloud account, no telemetry). Their context is a focused work session at a desk, often with multiple sessions running in parallel; they jump between Agents the way other people jump between Slack channels.

The job to be done: orchestrate several AI coding agents side-by-side across multiple projects and remote machines without losing track of who is doing what, what's waiting on approval, and where each session's state lives.

## Product Purpose

A local-first desktop workspace (Wails v2 + Go + React) that treats each AI assistant as a first-class **Agent** with its own role, system prompt, skills, and backend engine. Agents are organized into **Departments** like a real org, run many **Sessions** in parallel, and can be driven from a unified chat UI or `⌘K` command palette without juggling terminal tabs. A companion daemon (`agentred`) lets the same chat run on a remote LAN machine while tool approvals and ask-user prompts surface back in the desktop UI.

Success looks like: a power user driving 5+ concurrent agent sessions across 2-3 projects from a single window, never losing context, never approving the wrong tool call, and never feeling the UI is in the way.

## Brand Personality

**Calm, precise, expert.** Linear-adjacent quiet confidence. The interface fades; the work — code edits, tool calls, agent state — is the star. Voice is direct and unadorned: short labels, no marketing language, no anthropomorphic warmth ("Let me help you with that!" is a failure mode). Density is welcome when it's earned; chrome is suspect unless it's load-bearing.

## Anti-references

Three failure modes the design must actively avoid:

1. **Generic ChatGPT / Claude.ai clone.** A single endless scroll with a centered hero chat input. No affordances for multi-agent, multi-session, multi-project work. Agentre is the opposite shape of that product; if a screen starts to resemble it, the multi-agent metaphor has collapsed.
2. **Enterprise SaaS dashboard.** Hero-metric cards, gradient charts, corporate blue, sidebars stuffed with rarely-used links. This audience reads that aesthetic as untrustworthy and out-of-touch.
3. **Cyberpunk / neon-on-black "AI" aesthetic.** Glowing accents, glassmorphism, neon gradients, tech-bro futurism. Power-user developers reject this on sight; it signals shallow product depth.

## Design Principles

1. **The work is the star, not the chrome.** Every visible element earns its place by directly supporting the agent/session/tool-call workflow. Decorative gradients, glass effects, and hero-metric templates are banned by default.
2. **Keyboard-first, mouse-tolerant.** `⌘K`, shortcuts, and arrow-key navigation are first-class. Anything reachable only by mouse-hover is a bug. Power users should be able to drive the app blindfolded.
3. **Multi-agent is the shape, not a feature.** The default view always answers "what are my agents doing right now?" The UI never collapses into a single-chat assumption, even when only one session is open.
4. **Local-first honesty.** No fake cloud chrome, no telemetry banners, no "Sign in" affordances. The interface reflects that data lives in `~/Library/Application Support/agentre/` and that the user owns it.
5. **Information density with hierarchy, not noise.** Linear-style: a lot fits on screen, but scale + weight + tinted neutrals make scanning easy. Cards are used only when they're truly the best affordance; nested cards are always wrong.

## Accessibility & Inclusion

- **Target:** WCAG 2.1 AA across all primary surfaces (contrast ratios, focus visibility, keyboard reachability). Inferred from the keyboard-first / power-user audience; adjust if you want stricter (AAA) or a different floor.
- **Reduced motion:** Respect `prefers-reduced-motion`. Motion is purposeful (state transitions, peek-ins) and short; never decorative.
- **Color independence:** Status, severity, and agent identity must be readable without color (icon, label, weight). Agent department color is a secondary signal, never the only one.
- **Internationalization:** UI ships in zh-CN and en — copy must avoid idioms that don't translate (no "let's dive in", no "hit the ground running"). Both registers should feel equally native, not translated.
