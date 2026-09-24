# ASIDE
## A payment link for your time.

**The complete product, design and engineering brief for the coding agent.**  
Specification date: **22 September 2026** · Initial market: **Nigeria** · Initial currency: **NGN**  
Release target: **a polished, working private beta—not a landing-page mockup.**

> “Can I pick your brain?”  
> “Sure. Here’s my link.”

That exchange is the product. Protect it.

**Naming note:** `Aside` is a working name chosen to give the design a coherent identity. It is not a confirmed company name, registered trademark or purchased domain. All examples use the reserved illustrative domain `aside.example`. Keep the name, wordmark text, domain, support address and legal entity configurable. Do not buy a domain, register a business or publish under this name without the founder’s approval. This is a separate product from Kredit; do not reuse Kredit’s merchant account, legal identity, database or customer data without explicit authorization.

---

## Read this before writing code

You are building the public website, personal time links, guest booking flow, offer flow, authentication, logged-in application, operational admin, backend, database, payment integration, notifications and growth measurement described here.

Do not interpret this as permission to create a beautiful homepage and leave everything else as stubs. Equally, do not turn a simple product into an enterprise platform. The visible experience must stay small; the engineering behind a real payment must be correct.

Follow the numbered implementation stages in Section 35. Maintain `docs/IMPLEMENTATION_STATUS.md` as you work. Distinguish **implemented**, **verified locally**, **verified in provider sandbox**, **blocked by external approval**, and **not started**. Never substitute a fake success screen for a missing integration.

Before changing an existing repository, inspect its structure and preserve useful work. For a fresh repository, use the stack and structure in this document. Resolve ordinary implementation choices yourself. Record material deviations in `docs/DECISIONS.md`; do not quietly change the product.

**When requirements compete, use this order:**

1. Financial correctness, security, applicable obligations and honest product claims.
2. The founder’s product constraints below.
3. A complete end-to-end transaction.
4. Visual quality and mobile usability.
5. Conversion experiments and optional polish.

No production payments, external invitations, mass messages, provider onboarding submissions or public deployment without explicit owner approval. Local development and sandbox integrations should proceed without inventing credentials.

### Navigation

- [01 — What we are building](#01--what-we-are-building)
- [02 — Scope and release boundaries](#02--scope-and-release-boundaries)
- [03 — Brand and writing](#03--brand-and-writing)
- [04 — Art direction](#04--art-direction)
- [05 — Design system](#05--design-system)
- [06 — Components and interaction](#06--components-and-interaction)
- [07 — Route inventory](#07--route-inventory)
- [08–14 — Public pages, booking, offers and onboarding](#08--the-homepage)
- [15–19 — Logged-in application, scheduling, sharing and admin](#15--the-logged-in-application)
- [20–25 — Architecture, data, API and authentication](#20--technology-and-architecture)
- [26–29 — Payments, payouts, exceptions and notifications](#26--payments-and-the-five-percent-promise)
- [30–34 — Growth measurement, operations and delivery quality](#30--growth-measurement-without-invented-virality)
- [35–39 — Build stages, verification, launch and handoff](#35--step-by-step-build-order)
- [40 — Primary reference register](#40--primary-reference-register)

---

## 01 — What we are building

### The premise

Someone already knows the person they want to speak with. Perhaps they follow them online. Perhaps someone introduced them. Perhaps they have seen what that person has done. They ask for a conversation. The person sends one link. The visitor either pays the listed price for a block of time, or makes an offer for that time.

**The visitor does not need us to recommend a person. The person does not need us to package their expertise.**

This is not about charging friends for every conversation. The first use case is an unsolicited request from a stranger or weak acquaintance to someone whose time they value.

### Non-negotiable product principles

| Principle | Consequence in the product |
|---|---|
| They already know you. | No expertise questionnaire, service catalogue, categories, portfolio or sales biography. |
| Anyone with time someone values can participate. | No requirement to be a professional, coach, mentor, influencer or certified expert. This does not permit unlicensed regulated services. |
| Your price, or their offer. | Two clear pricing modes. No auction, public bidding or complicated packages. |
| One personal link. | A clean `/{handle}` route is the core product, not an add-on to a storefront. |
| Buyers should not need a full account. | Guest checkout and secure booking access. Seller creation remains an explicit, optional action. |
| Simple visible setup. | Claim a link first; complete the minimum readiness steps before collecting money. |
| Charges must not exceed the founder’s 5% ceiling. | Enforce the cap in the backend. Never hide extra platform fees in checkout. See Section 26 for the proposed conservative fee interpretation. |
| Sellers should get their money quickly. | Use provider-supported settlement; do not invent instant settlement or require a withdrawal button. |
| We are not a satisfaction-arbitration marketplace. | No ratings tribunal or “I disliked the advice” refund workflow. Objective payment exceptions and unavoidable provider processes still exist. |
| Growth should come from ordinary use. | Sharing and buyer-to-seller activation belong in the core flow. No contact scraping, automatic social posts or fake referral activity. |

### The two purchase modes

**Set my price**

A person enters a price for 30 minutes. They choose whether 15-, 30- and 60-minute conversations are available. Other enabled durations are calculated transparently from that base rate. Thirty minutes is the default selection.

**Make me an offer**

A visitor chooses a duration and proposes an amount. The person accepts, declines or returns one counteroffer. No money is collected until both sides agree to a price and the visitor chooses an available time.

V1 exposes **one pricing mode at a time** on a person’s page. The seller can change modes for future requests. Accepted offers and existing bookings retain their original terms.

### What success actually means

The first success is a real request becoming a real payment and a real conversation. The second is that the seller does it again. The growth hypothesis is that a buyer then creates their own link and receives a genuine payment from someone else.

Do not claim that share buttons guarantee virality. Measure whether that propagation happens, how often it happens, and how long it takes.

---

## 02 — Scope and release boundaries

### Build for private beta

Public home, pricing and help pages; personal links; fixed-price booking; offer negotiation; guest booking management; email-based account access; seller onboarding; weekly availability and date overrides; booking-specific meeting links; a shared buyer/seller account; earnings and settlement status; sharing; minimal admin; genuine payment reconciliation; provider exceptions; privacy controls; transactional email; first-party growth events.

### Keep out of V1

Public discovery, search for people, professional categories, courses, ebooks, downloads, subscriptions, retainers, group sessions, public follower counts, public reviews, recommendations, paid messages, document review products, built-in video, call recording, per-minute billing, “available now,” open-ended wallets, peer-to-peer money transfers, native mobile apps, affiliate rewards, cash referral bonuses, AI chat, AI-generated biographies and a page builder.

Do not quietly reintroduce these through “helpful” onboarding questions.

### Three delivery levels

| Level | What must work | What must not be implied |
|---|---|---|
| Local demonstration | Entire product with clearly labeled synthetic people, payments and messages. | No real payouts, actual adoption or provider approval. |
| Private beta | Real approved merchant integration, required onboarding, genuine booking and settlement records, security controls, basic operations. | No guaranteed instant settlement or verified conversation attendance without evidence. |
| Expansion | Optional calendar OAuth, an approved expedited payout route, more payment channels and measured growth experiments. | These are not prerequisites for showing a strong first product. |

The owner asked for a complete brief. That does not mean every later feature should be built before the first beta.

---

## 03 — Brand and writing

### Working identity

**Name:** Aside  
**Descriptor:** A payment link for your time.  
**Main line:** People want your time. Give them a link.  
**Human line:** “Can I pick your brain?” has a new reply.

The personality is thoughtful, assured and lightly playful. It is not a corporate finance platform and not a loud “make money online” scheme. It should be comfortable for a 24-year-old developer and a 50-year-old business owner to share the same link.

### Writing rules

Use short, spoken English. Describe the action or outcome. Avoid pep talks. Humor is optional and belongs in sharing, not payment failures or security messages.

Never write:

- “Unlock your potential.”
- “Empower your journey.”
- “Monetize your expertise seamlessly.”
- “Revolutionize the way you connect.”
- “The ultimate all-in-one platform.”
- “Join thousands of satisfied experts,” unless independently true and approved—and even then prefer better copy.

Do not fill empty screens with paragraphs. Do not repeat the word “seamless.” Do not tell users the design is beautiful. Show care through the interface.

### Core copy bank

| Context | Approved direction |
|---|---|
| Homepage headline | People want your time. Give them a link. |
| Homepage supporting text | Set your price—or let them make an offer. When someone asks to talk, send your link. |
| Main CTA | Get my link |
| Secondary CTA | See how it works |
| Public page heading | A little time with {first_name}. |
| Fixed price CTA | Pick a time |
| Offer CTA | Make an offer |
| Checkout CTA | Pay {formatted_total} |
| Offer confirmation | Your offer is with {first_name}. We’ll email you when they reply. |
| Offer accepted | {first_name} accepted. Pick a time and pay to book it. |
| Payment pending | We’re checking your payment. You don’t need to pay again. |
| Booking confirmed | You’re booked with {first_name}. |
| Seller first payment | Your first paid booking. |
| Seller payout label | On its way to your bank |
| Draft link success | Your link is ready. Finish setup to take bookings. |
| Buyer-to-seller prompt | People ask for your time too? |
| Buyer-to-seller CTA | Get my own link |
| Paused profile | {first_name} isn’t taking bookings right now. |
| Empty bookings | Your first booking will appear here. |
| Empty offers | No offers yet. Your link is ready to share. |
| Fee disclosure, after commercial approval | You keep 95%. No subscription. |

The 95% wording is enabled only when the actual approved arrangement supports it. Do not ship marketing copy ahead of commercial reality.

### Content implementation

Keep interface copy in structured source files by domain, not scattered throughout templates. Use interpolation with localization-safe formatting. Keep payment and policy wording versioned. Use a shared `BrandConfig`, but do not permit administrators to inject arbitrary HTML or JavaScript into pages.

---

## 04 — Art direction

### The visual idea: a personal invitation, carefully printed

Imagine a well-made invitation on warm paper: a generous serif headline, a clear photograph, a confident green button, a date written plainly, and nothing fighting for attention.

The product should feel **personal, tactile and precise**. Warm ivory instead of hospital white. Forest green instead of generic electric blue. One sharp citron accent used sparingly. Fine rules, carefully aligned numbers, restrained shadows and excellent typography.

The website should not look like an AI-generated SaaS template.

### What the first screen should feel like

On desktop, the left side carries a striking, four-line editorial headline. On the right is a large, believable personal booking page, not a collage of dashboard cards. A small two-message exchange introduces the behavior. The composition has room to breathe. A thin vertical rule quietly separates the editorial side from the product demonstration.

There is one dominant action: claim your link. Everything else supports that action.

On mobile, the headline and claim field come first. A compact working demonstration follows. Do not reduce the type to make a desktop composition fit; recompose the page.

### A signature visual detail

Use a small **notched appointment slip** in booking confirmations and share previews: a light paper surface, an inset date column, one hairline divider, and tiny opposing semicircular notches at the divider. This is a recurring motif, not a decorative trick on every card.

On a public person page, a slim “conversation line” joins their avatar and the time selector. It can be a quiet vertical rule, not a squiggle animation. Reuse the geometry in the wordmark mark: two simple facing rounded strokes, suggesting a conversation. If the mark is not genuinely good, use the wordmark alone.

### Explicit visual prohibitions

No purple-blue gradients. No neon glow. No translucent glass cards. No floating 3D brains. No decorative crypto-style coins. No generic dashboard mockups in the hero. No six-card bento grid. No endless pill labels. No four-column feature icon section. No fake customer logos. No fabricated testimonials. No stock photographs of people pointing at laptops. No confetti at every click. No text on low-contrast beige backgrounds.

Do not make every element rounded. Different levels of shape create hierarchy: nearly square data rows, gently rounded panels, and rounded primary controls.

### Five visual acceptance scenes

1. **Homepage at 1440 × 1000:** the headline, domain claim control and booking preview read as one designed composition, not separate components placed in a grid.
2. **Personal link at 390 × 844:** the person, price or offer field, duration and next action are immediately obvious. No marketplace biography competes with them.
3. **Booking confirmation at 390 × 844:** receipt and meeting instructions are unmistakable; the growth prompt sits below them, not over them.
4. **Seller overview at 1440 × 1000:** today’s work and the personal link come before charts. It looks calm even with real activity.
5. **Admin payment detail at 1440 × 1000:** a payment can be understood from one timeline, one amount breakdown and one settlement section. The page should not resemble a marketing dashboard.

---

## 05 — Design system

### Color tokens

Use semantic tokens. Do not scatter hexadecimal colors through components.

```css
:root {
  --paper: #f7f4ec;
  --surface: #fffdf8;
  --surface-muted: #eeeee4;
  --ink: #202b25;
  --ink-secondary: #4d5b51;
  --ink-muted: #626b63;
  --line: #dadfd2;
  --line-strong: #7d8978;
  --forest: #234e3b;
  --forest-hover: #193c2c;
  --citron: #dfff80;
  --citron-ink: #26351b;
  --success: #256244;
  --success-surface: #eaf2e8;
  --warning: #795019;
  --warning-surface: #fff1d4;
  --danger: #a3342d;
  --danger-surface: #fbeae5;
  --focus: #365bdb;

  --radius-control: 12px;
  --radius-panel: 20px;
  --radius-sheet: 24px;
  --shadow-soft: 0 12px 36px rgb(32 43 37 / 7%);
  --shadow-float: 0 24px 72px rgb(32 43 37 / 10%);

  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-5: 20px;
  --space-6: 24px;
  --space-8: 32px;
  --space-10: 40px;
  --space-12: 48px;
  --space-16: 64px;
  --space-20: 80px;
  --space-24: 96px;
}
```

Verify contrast for each actual foreground/background combination. Use `--line-strong` for control boundaries when the boundary is needed to identify the input; keep `--line` for decorative separators. Border colors are not automatically suitable for text or focus indicators. Citron is an accent surface with dark text, not body text on ivory. Error and status states need words or icons as well as color.

### Typography

**Display:** Newsreader, variable, using a licensed, self-hosted WOFF2 build.  
**Interface and body:** Manrope, variable, using a licensed, self-hosted WOFF2 build.

Verify the specific files’ licenses and retain license notices. Font choice is a design requirement; do not copy a font from a commercial reference site. Use a restrained system-serif/system-sans fallback if approved font files are unavailable.

| Style | Desktop | Mobile | Notes |
|---|---:|---:|---|
| Home display | 80–104px | 46–58px | Newsreader, 0.96–1.04 line height; tune optical sizing. |
| Page display | 48–64px | 36–44px | Serif. Not used for financial tables. |
| Section heading | 32–44px | 28–34px | Serif or restrained sans by context. |
| App title | 28–32px | 26–30px | Manrope, 600. |
| Body | 17px | 16px | 1.5–1.65 line height. |
| Control label | 14–15px | 14–15px | 600; never all caps for normal fields. |
| Small supporting text | 13–14px | 13–14px | Still comfortably readable. |
| Money and times | Contextual | Contextual | Tabular numbers. Preserve the naira sign. |

Use italic Newsreader in at most one short emphasis per marketing composition. Do not italicize entire headlines. No uppercase, widely tracked body paragraphs.

### Layout

Marketing maximum width: `1248px`. App content maximum width: `1120px`. Personal booking page maximum width: `980px` on desktop, with a narrow identity column and a clear main booking area. Mobile gutter: `20px`; narrow 320px viewport gutter: `16px`. Desktop gutters: `32–64px`.

Desktop marketing grid: 12 columns with `24px` gaps. Homepage hero: roughly 6 columns editorial, 1 column breathing room, 5 columns product. Marketing sections use `88–128px` vertical spacing on large screens and `48–64px` on mobile.

Controls: minimum `48px` high, primary booking buttons `52–56px`, generous touch areas. Input font size at least `16px` on mobile. Never depend on hover to reveal a required action.

The app uses an open canvas and rows before cards. Cards group meaningful content; they do not provide decoration for every number.

### Responsive requirements

Check `320`, `360`, `390`, `430`, `768`, `1024`, `1280` and `1440px`. Tablet is not a shrunken desktop. Public checkout becomes a single column with an accessible sticky bottom action only when it improves the flow. Reserve space so it never covers content or the keyboard.

Use dynamic viewport units where appropriate, safe-area insets and sensible keyboard behavior. Horizontal overflow is a bug except inside deliberately scrollable financial tables with a visible scroll affordance.

### Motion

Most transitions: `140–180ms`. Sheet/dialog transitions: `200–240ms`. Use opacity and small transforms; no layout thrashing or scroll hijacking. The hero demonstration changes only in response to interaction; no endless autoplay.

Respect `prefers-reduced-motion`. Focus should move correctly when a sheet opens or a booking step advances. A successful payment gets one quiet checkmark transition, not a fireworks show.

---

## 06 — Components and interaction

Build a small, coherent component library first. Accessible primitives may come from shadcn-svelte/Bits UI, but the shipped appearance must use this design system rather than the default theme. Confirm current installation requirements in references F2–F3.

| Component | Required behavior |
|---|---|
| `BrandMark` | Configurable wordmark; accessible name; compact and full variants. |
| `Button` | Primary, secondary, text, destructive; loading preserves width; native disabled semantics. |
| `MoneyInput` | Naira formatting, integer-minor-unit parsing, no floating-point calculations. |
| `DurationPicker` | Accessible radio group; enabled durations only; price update announced politely. |
| `ModePicker` | Fixed price / offers; explanation in one sentence, not a feature comparison. |
| `LinkClaimField` | Static domain prefix; handle validation; availability result; no premature reservation claim. |
| `IdentityHeader` | Avatar or initials, display name, handle; no made-up verification badge. |
| `DateStrip` | Keyboard-operable dates with availability counts if real; full date accessible label. |
| `TimeGrid` | Timezone visible; actual slots; keyboard support; no fake unavailable times for urgency. |
| `BookingSummary` | Person, duration, exact date, timezone, amount and payment state. |
| `AppointmentSlip` | Confirmation motif; selectable text; accessible reading order. |
| `SharePanel` | Copy, native share when supported, deliberate WhatsApp/X actions, card download. |
| `OfferCard` | Amount, duration, expiry and actor; clear accept/counter/decline actions. |
| `StatusPill` | Text plus semantic color; consistent state vocabulary. |
| `ActivityRow` | One action/event, time and related record; no unnecessary nested cards. |
| `AmountBreakdown` | Gross, deductions, seller entitlement, provider cost visibility by permission. |
| `SettlementTimeline` | Payment received / allocated / settlement scheduled / sent or failed. |
| `InlineNotice` | Success, warning, error; persistent important messages are not transient toasts. |
| `EmptyState` | Plain sentence and one useful action; no generic robot illustration. |
| `ConfirmDialog` | Describes the actual consequence; no generic “Are you sure?” only. |
| `SensitiveReveal` | Masked by default; permission, reason and audit where necessary. |
| `DataTable` | Server pagination, accessible sorting, empty/error/loading states, mobile alternative. |
| `FilterBar` | Filters persisted in query parameters; clear reset; no hidden filter state. |
| `StateBoundary` | Intentional loading, empty, error, permission and unavailable variants. |

Create an internal development-only component gallery at `/dev/ui`, inaccessible in production. Show every component state there. Do not include a production design-system route in public navigation.

### Universal interaction rules

A disabled payment button must explain what is missing. A failed form must retain input. Amount/date changes must update the summary. Browser back navigation must not silently erase the selected person or restart payment. Never optimistically mark a payment, payout or offer acceptance as successful.

Use skeletons only when content is genuinely loading. Do not add artificial delays to make transitions look sophisticated.

---

## 07 — Route inventory

Treat this as an implementation checklist. Every route requires responsive layout, correct access rules, intentional errors and working primary actions.

### Public and transactional

| Route | Purpose |
|---|---|
| `/` | Homepage and interactive example. |
| `/pricing` | Honest fee explanation and an illustrative amount calculator. |
| `/help` | Short questions organized around getting paid, booking and access. |
| `/terms`, `/privacy`, `/acceptable-use` | Versioned policy pages; approved text required before live payments. |
| `/login` | Email sign-in; no separate signup/login maze. |
| `/auth/verify` | OTP entry and safe verification flow. |
| `/claim` | Claim a personal link; resumes after sign-in. |
| `/{handle}` | Personal time page: fixed price or offers. |
| `/{handle}/book` | Time selection and guest checkout. |
| `/{handle}/offer` | Offer creation. |
| `/checkout/{checkout_id}` | Resume a scoped checkout; never public by ID alone. |
| `/payment/return` | Payment return; triggers backend verification, not success by query string. |
| `/booking/{public_id}` | Private guest/account booking detail. |
| `/booking/{public_id}/reschedule` | Secure rescheduling request. |
| `/offer/{public_id}` | Private offer thread, counteroffer and next action. |
| `/access` | Request secure access to a booking/offer using verified email. |
| `/r/{share_id}` | Optional non-PII share attribution redirect to a personal link. |
| `/og/{handle}/{version}.png` | Sanitized, cacheable public preview image. |
| `/health` | Minimal non-sensitive public health response, not infrastructure detail. |

### Authenticated application

| Route | Purpose |
|---|---|
| `/app` | Role-aware overview. |
| `/app/bookings` | Giving time / booking someone tabs. |
| `/app/bookings/{id}` | Booking detail and permitted actions. |
| `/app/offers` | Received / sent; received only when a seller profile exists. |
| `/app/offers/{id}` | Offer detail and negotiation. |
| `/app/link` | Personal page settings and live preview. |
| `/app/availability` | Weekly hours and date overrides. |
| `/app/money` | Earnings and real settlement status—not a wallet. |
| `/app/money/{id}` | Allocation/settlement detail. |
| `/app/share` | Copy/share link and optional share-card export. |
| `/app/settings` | Account, identity, notification preferences and privacy. |
| `/app/settings/payouts` | Provider onboarding and payout destination. |
| `/app/settings/security` | Sessions, MFA/passkeys if enabled, account recovery. |
| `/app/settings/connections` | Meeting delivery settings; optional calendar connection. |
| `/app/onboarding` | Resumable readiness checklist. |

### Operations

`/ops`, `/ops/people`, `/ops/people/{id}`, `/ops/bookings`, `/ops/bookings/{id}`, `/ops/payments`, `/ops/payments/{id}`, `/ops/settlements`, `/ops/exceptions`, `/ops/exceptions/{id}`, `/ops/growth`, `/ops/system`, `/ops/audit`, `/ops/settings`, `/ops/access`.

Reserve all system route words so nobody can claim handles such as `api`, `app`, `ops`, `admin`, `login`, `claim`, `help`, `pricing`, `terms`, `privacy`, `booking`, `offer`, `checkout`, `payment`, `auth`, `r`, `og`, `access`, `health`, `dev`, `settings`, `support`, `www` or `assets`. Match the public handle route explicitly, not through an unrestricted catch-all.

## 08 — The homepage

### Header

A slim, `80px` desktop header on the paper background. Wordmark left. “How it works” and “Pricing” in the middle/right. “Log in” as a text link. “Get my link” as the only filled action. On mobile, wordmark, login and one compact CTA; do not build a full-screen menu for three items.

### Hero

Small introductory line: **For the “can I call you?” messages.**

Headline, deliberately art-directed:

> People want  
> your time.  
> Give them  
> a link.

The exact line breaks adapt by breakpoint. One short phrase may be italicized. Supporting text:

> Set your price—or let them make an offer. When someone asks to talk, send your link.

Below it, the claim control:

```text
aside.example/ [yourname                 ]  [Get my link]
```

On mobile, put the button below the field at full width. Under the field: **No course. No storefront. Just your time.** Do not place an unverified “30-second setup” promise here. Fast setup is a design target, not yet a measured claim.

### The interactive example

A real UI composition using the same public-page components, with a fictional person named **Tomi** and an obvious small “Example” label. Use an initials avatar unless licensed portrait imagery is supplied.

```text
Tomi
A little time with Tomi.

[15 min]  [30 min]  [60 min]
₦10,000 · 30 minutes

[Pick a time]
```

A discreet switch above the example changes between **Set a price** and **Take offers**. Visitors can change duration and see an illustrative time-selection step. No example payment should initialize a provider transaction. If they press the final example action, explain that it is an example and offer to create their own link.

Do not use a screenshot when a small working component is possible. Do not show unreadable pretend text in a phone frame.

### Behavior section

Use a two-message composition on the left and a short explanation on the right.

> “Can I pick your brain this week?”  
> “Of course. Pick a time here.”

Show the example link as an actual link card. Headline: **Same conversation. One less back-and-forth.** Body: **They already know why they want to talk to you. Your link handles the time and the payment.**

### Three-step explanation

Use a thin horizontal line connecting three numbered blocks on desktop; vertical on mobile. Not three large boxed cards.

**01 — Set your terms.** Name your price, or take offers.  
**02 — Send your link.** In a message, your bio, wherever people reach you.  
**03 — Make time.** They book. You talk. Payment follows the settlement schedule shown in your account.

Final copy should remain concise. The payment clause can move into a small linked note after the provider arrangement is finalized; never replace it with an unsupported “paid instantly.”

### Fee section

A dark forest horizontal section, not a separate pricing-card grid. Large “5% maximum.” Supporting explanation reflects the approved fee policy. An example amount calculator makes the result tangible. The section must not imply 5% is net platform profit.

### Closing section

Headline: **The next time someone asks, send this.**  
Repeat the claim field once. No email waitlist, app-store buttons or unrelated newsletter.

### Footer

Wordmark, one-line descriptor, help, pricing, terms, privacy, acceptable use and a real support contact. Show the legal entity only after it has been supplied and approved. No invented office address or founder biography.

---

## 09 — Pricing, help and policy pages

### Pricing

Headline: **Your time. Your price. A small fee when you’re paid.**

Display the percentage ceiling prominently. No subscription tiers in V1. Explain who pays the fee, what the seller receives, and whether processor charges are included. Use a calculator whose formula comes from the backend fee-policy endpoint, not separately hardcoded JavaScript.

For the conservative default in Section 26:

```text
Someone pays             ₦10,000
Total seller deduction      ₦500
You receive               ₦9,500
```

Under it, explain that the platform pays applicable processing costs from its share. Do not publish this interpretation until the owner has approved it and the chosen payment route supports it.

Discuss settlement timing honestly. “Payment received” and “money in your bank” are different events. If using standard Paystack settlement, the page must reflect the actual merchant schedule, not “instant.” Reference P1 documents the standard Nigeria schedule; account-specific terms must still be confirmed.

### Help

Start with eight useful questions, not a knowledge base:

- Do I need to be a professional? No. You do need to comply with rules applicable to what you provide.
- Do I have to say what I can help with? No. People opening your link already chose you.
- Can I choose the price? Yes, or choose offer mode.
- Does the visitor need an account? Not a full seller account; secure email access may be required.
- When does the money reach my bank? Show the approved route’s actual terms.
- Where does the conversation happen? In the private meeting link provided for that booking.
- Can I pause my link? Yes; existing bookings remain accessible.
- What happens if a payment or booking goes wrong? Explain objective exceptions and the contact route without promising satisfaction arbitration.

### Policies

Create polished policy-page layouts with a readable `72ch` maximum measure, contents navigation, version and effective date. Draft practical text, but label unapproved policy content in staging. Live collection is blocked until approved terms, privacy, acceptable use, seller obligations and the correct contracting/payment roles are present.

Do not claim that “all sales are final” eliminates payment-provider processes or applicable consumer rights. Do not treat the product descriptor as a legal determination of merchant-of-record status.

---

## 10 — The personal time link

### Fixed-price desktop layout

Left identity column, about `300px`: avatar `88–104px`, display name, handle, and a small brand link. No expertise list, job title, follower count, reviews or sales paragraph.

Right main area, about `540px`: “A little time with {first_name}.” Duration selector. Price. “Pick a time” button. A short explanation of the call method and the secure-payment step. The two columns align on an invisible baseline rather than sitting inside a giant card.

On mobile, identity becomes a compact top section. The duration and price follow immediately. The next action should usually be visible without scrolling on a normal phone, but do not force a cramped layout to achieve that.

### Fixed-price behavior

The displayed price is calculated from the server’s active pricing version. Changing the duration updates the total and the next available times. Use a skeleton or progress indicator only during an actual request.

The public page must not leak bank details, private email, phone number, calendar event titles, guest identities or meeting URLs.

### Offer-mode layout

Keep the same visual identity, but replace the price area:

```text
A little time with Tomi.

How long?
[15 min]  [30 min]  [60 min]

Your offer
₦ [                    ]

[Add a note — optional]

[Send offer]

You won’t be charged unless Tomi agrees and you book.
```

The note is optional, collapsed by default, plain text and limited to 280 characters. It is not an expertise questionnaire. No attachment upload or full messaging thread.

### Identity and credibility

Identity assurance is separate from expertise. A provider onboarding result does not prove someone is qualified in a field. Never show a generic “verified expert” tick. In V1, omit a public badge unless its exact meaning is clear and substantiated.

A seller may add **one optional identity link** to their own social profile. Validate it and label it by destination. It is not a requirement to describe what they know.

### Required public states

| State | UI behavior |
|---|---|
| Published and ready | Show the configured transaction mode. |
| Claimed but not payment-ready | Identity and a neutral “Not taking bookings yet.” No payment form. |
| Paused | Identity and “{name} isn’t taking bookings right now.” |
| No available slots | Explain that no times are currently open; do not invent scarcity. |
| Suspended | Neutral unavailable page; do not publicly reveal investigation details. |
| Unknown handle | Designed 404 and a restrained route to create a link. |
| Renamed handle | Redirect only through a controlled, retained alias owned by the same person. |
| Seller viewing their own page | Small private “Edit your link” control; no analytics contamination. |
| Unsupported currency/payment condition | Explain the limitation before collecting details or money. |
| Loading failure | Retry action; preserve the person and any non-sensitive selections. |

Do not render “only two slots left” unless that statement has a clear, genuine meaning. Even then, it is not needed for this product.

---

## 11 — Fixed-price guest booking

### Step 1: choose a duration and time

Load actual server-generated availability. Display the visitor’s detected IANA timezone with an editable selector. Show full date, weekday, year where helpful, and timezone in the summary. Nigerian users may default to `Africa/Lagos`, but do not hardcode UTC+1 for every user.

On desktop, show a compact month/date selection and adjacent time grid. On mobile, a scrollable date strip plus clearly grouped morning/afternoon/evening slots is acceptable. Include a full calendar option for later dates. Keyboard use must be first-class.

Selecting a time does not guarantee it forever. The server creates a short-lived hold when the buyer proceeds to checkout, not merely when a date is viewed.

### Step 2: details

Fields: **Your name**, **Email**, and a collapsed optional note. No password. No occupation. No phone number unless the selected delivery method genuinely needs it. No marketing checkbox preselected.

Create a restricted guest checkout session. It is not a seller account and must not grant access to other records attached to the entered email. A returning browser can resume only checkouts it is authorized to access.

For normal fixed-price checkout, do not force a full signup. Email ownership must be verified before cross-device management, linking purchases to an account or treating the buyer as a verified identity in growth metrics. Payment alone is not email verification.

### Step 3: review and pay

Show the person, duration, exact date/time/timezone, total, payment terms and seller identity required by the approved legal arrangement. The CTA includes the total: **Pay ₦10,000**.

The backend creates a quote with immutable amount, fee policy, pricing version, seller and slot. It initializes provider checkout with that quote. The browser cannot set the fee, seller payout account or authoritative amount.

Use provider-hosted checkout in V1. Do not build a card form or handle card numbers. Show only payment channels actually enabled for the merchant and supported by the commercial/risk policy. Do not list every provider logo merely because the provider has a global product page.

### Step 4: return and verification

The provider redirect is not proof of success. Show **We’re checking your payment.** Query the backend, which verifies provider state and matches the reference, amount, currency, merchant environment and quote.

When success is established and the slot is secured, show confirmation. If the browser closes, the authenticated webhook and reconciliation worker must still finish processing.

### Step 5: confirmation

The appointment slip shows:

- “You’re booked with Tomi.”
- Date and timezone, duration and amount paid.
- Meeting link if ready; otherwise the precise next step and delivery deadline.
- Add to calendar and manage booking.
- Receipt access.

Below the transaction information, separated by a generous rule:

> **People ask for your time too?**  
> Next time, send your own link.  
> **Get my own link**

Do not open a modal over the confirmation. Do not automatically create a public profile. Do not imply an entered or suggested handle has been reserved until the user actually claims it.

### Pending, failed and late payments

Do not label a payment failed merely because the browser request timed out. A pending payment remains pending until reconciled. A retry action must not accidentally create two charges for one booking.

If a payment arrives after a hold expires, attempt to reacquire the same slot atomically only if it is still valid and free. Otherwise record **paid—needs attention**, explain the situation, and enter the objective exception process. Never silently book a different time or pay a seller twice. Automatic provider split settlement may already be in progress; the interface must not pretend the application can always stop it.

---

## 12 — Offers without unnecessary negotiation

### Initial offer

The visitor chooses duration and amount, optionally adds a note, then supplies name and email. Verify email ownership before delivering an offer to the seller; this protects against nuisance offers and impersonation. Offer access uses a scoped guest session, not a required seller account.

No charge or authorization is taken at this stage. Do not create card holds speculatively. Display a genuine expiry, initially 48 hours, configured server-side.

### Seller response

The seller sees amount, duration, sender name, optional note and expiry. Actions:

- **Accept offer**: agrees to the amount; sends the visitor a booking/payment link.
- **Make a counteroffer**: one amount field, same duration, optional short note.
- **Decline**: ends the request without charging anyone.

V1 permits one seller counteroffer. The visitor can accept or decline it. Do not build a chat product, repeated bidding loop or auction.

### Agreement and booking

An accepted price is valid for 24 hours by default. Acceptance does not reserve a particular slot. The visitor chooses from current availability and pays. State this plainly: **Pick a time and pay to confirm.**

The private booking link is bound to the agreed offer, seller, duration and verified guest. It cannot be used to buy a different duration at the old price. Agreement is not a payment; payment is not necessarily bank settlement.

Once one verified payment converts an offer to a booking, the offer cannot create another paid booking. Old counteroffer versions cannot be accepted after a newer terminal action.

### Offer state machine

```text
DRAFT
  -> PENDING               (verified visitor submits)
  -> WITHDRAWN             (visitor cancels before submission)

PENDING
  -> AGREED                (seller accepts)
  -> COUNTERED             (seller sends the one counteroffer)
  -> DECLINED              (seller declines)
  -> WITHDRAWN             (visitor withdraws)
  -> EXPIRED               (deadline passes)

COUNTERED
  -> AGREED                (visitor accepts current counteroffer)
  -> DECLINED              (visitor declines)
  -> WITHDRAWN             (seller withdraws counteroffer)
  -> EXPIRED

AGREED
  -> CONVERTED             (one verified payment + confirmed booking)
  -> EXPIRED               (agreement purchase window passes)
  -> WITHDRAWN             (permitted before payment initialization)
```

During an active checkout, a withdrawal must serialize against payment initiation and pending payment resolution. Do not invalidate a quote under a payment already in flight. Use row versions and database transactions, not frontend button disabling, to enforce this.

---

## 13 — Private booking and conversation management

### Guest access

A public-looking booking ID is not an access credential. The current restricted checkout session can see its own booking. Cross-device access requires email verification or a scoped, expiring access token exchanged for a restricted session. Avoid sensitive tokens in analytics, logs and third-party requests.

An access email opens a neutral page. Do not consume a one-time token solely because a mail scanner follows a GET request. Complete exchange on an explicit action or use a safely designed verification flow.

### Booking page

Show the person, date/time, duration, price, payment state, meeting delivery state and permitted actions. The seller sees a different action set from the buyer. Both use the same canonical booking record.

Actions include add to calendar, copy the private meeting link, request a reschedule, contact the other participant through the permitted booking channel, retrieve receipt and report an objective issue.

### Rescheduling

Keep the original booking valid until both sides agree to a replacement. A request proposes a new time but does not create an indefinite hold. On acceptance, recheck and reserve the new slot in the same database transaction that releases the old reservation.

The original duration, price and payment remain unchanged. V1 does not support paid upgrades inside rescheduling. If the proposed slot is no longer free, show that honestly and ask for another time.

Send updated calendar data with the same event UID and an incremented sequence. Cancel old reminders. Preserve the full history.

### Cancellations and non-delivery

A seller can request cancellation but cannot delete the booking or its financial history. A cancellation with money collected enters the objective exception rules in Section 28. A buyer cancellation follows the disclosed booking terms and any applicable overriding obligations.

Do not present a universal “no refund under any circumstances” switch. Do not promise that clicking “Join” proves someone attended.

### Conversation completion

After the scheduled end, mark the time window **elapsed**, not automatically **verified completed**. A participant may confirm it happened. Store whether the completion signal came from one participant, both participants or an approved meeting provider. Keep “time elapsed,” “link clicked” and “conversation confirmed” separate in data and analytics.

Payout timing is not tied to a subjective five-star review or a buyer satisfaction vote.

---

## 14 — Sign-in, claiming a link and seller readiness

### One account, two directions

A person can buy someone’s time and sell their own. Do not create separate buyer and seller accounts. A seller profile is an optional capability attached to a verified user.

V1 sign-in uses email OTP. Use a reputable email service and a well-reviewed authentication approach. Google sign-in is optional later; it must not be a prerequisite for getting a link.

### Claim flow

**Screen 1 — Your link**

Headline: **Make it yours.** Handle field, real availability feedback and a small live preview. Handle rules: lowercase ASCII letters, digits and a single internal hyphen pattern; 3–24 characters; no leading/trailing/consecutive hyphens; reserved words blocked. Unicode is allowed in display names, not in V1 handles.

**Screen 2 — Your email**

Email and code verification. Preserve the intended handle through the flow, but explain if someone else genuinely claims it before the database reservation completes. Rate-limit reservations and prevent name squatting.

**Screen 3 — Your price**

Two choices: **I’ll set a price** / **Let people make an offer**. In fixed mode, one base amount for 30 minutes. Duration toggles below. In offer mode, enabled durations only; optional private minimum is a later control, not an onboarding requirement.

**Screen 4 — Your link exists**

Show the personal URL and preview. Copy/share is available, but the status is **Finish setup to take bookings** until readiness requirements are met. Do not pretend an incomplete payout profile can accept real money.

### Readiness checklist

A compact checklist with four substantive tasks:

1. Confirm display name and optional photo. Initials are a valid fallback.
2. Open some time: weekly hours or a few specific dates, with timezone.
3. Choose how the conversation will happen and acknowledge meeting-link delivery responsibilities.
4. Complete the payment provider’s required identity/business checks and verified payout destination.

Age and acceptable-use/seller-term acknowledgments are included where needed. V1 is for adults; do not launch a minors marketplace through this flow.

### First available schedule

Offer a sensible editable template rather than asking for an empty calendar. It must never be silently activated. Example: “Choose the days and times you want to offer.” The person explicitly confirms the timezone and hours.

A link can be claimed quickly. **Taking paid bookings is a separate readiness milestone.** Do not sacrifice financial onboarding just to claim a 30-second activation statistic.

### Ready state

Headline: **You’re open for bookings.** URL, copy button and native share. One small preview, one next action. No gamified dashboard tour, progress confetti or ten onboarding tips.

---

## 15 — The logged-in application

### App shell

Desktop: a quiet `224px` left rail with wordmark and navigation. Main content begins around `48px` from the rail edge. Top utility row contains page context, notifications if implemented, and account menu. No giant gradient banner.

Primary navigation: **Overview**, **Bookings**, **Offers**, **Money**, **Your link**. Availability and sharing can be subnavigation under Your link, with direct shortcuts from overview. Settings sits at the bottom.

Mobile: header with identity/menu; compact bottom navigation for **Home**, **Bookings**, **Offers**, **Link** when relevant. Money and settings remain easy to reach through the menu. Buyer-only accounts show their bookings and a modest “Get your own link” action instead of dead seller screens.

### Overview: seller

Lead with **Your link** in a useful, copyable row. Next show today’s or next upcoming conversation and unanswered offers. Earnings are a restrained secondary section, not the entire identity of the product.

Example layout:

```text
Good evening, Tomi.                        [View your page]

Your link
aside.example/tomi                         [Copy] [Share]

Next up
Thu, 24 Sep · 4:30 PM · Africa/Lagos
30 minutes with Ada                       [Open booking]

2 offers need a reply                     [View offers]

This month
4 paid bookings       ₦38,000 seller earnings
```

Time-sensitive greetings are optional; do not use them if they introduce hydration mismatch. Dates in seed/demo content are generated relative to the environment, not stale hardcoded “tomorrow” labels.

**Empty overview:** “Your link is ready.” Explain one next action: send it the next time someone asks for your time. Show no fake graph.

### Overview: buyer-only

Upcoming booked conversations and private booking history. After the useful content, one invitation to create a link. No assumed profession, public seller profile or earnings number.

### Bookings

Tabs: **Giving my time** and **Booking someone**. Filter by upcoming, past, canceled and needs attention. A compact list shows counterpart, date, duration, amount and relevant status. Do not mix payment state with attendance state in one misleading pill.

Booking detail has a clear summary, meeting section, payment information and activity history. Place secondary actions in a menu, but keep urgent missing-link or payment-attention actions visible.

### Offers

Tabs for received/sent and filters for waiting, agreed, closed. Desktop rows can open a detail pane; mobile opens a full detail page. Acceptance always shows the amount and duration again. Counteroffer input uses the same validated money component as public offers.

Unread indicators must reflect actual unread state, not merely a pending offer count.

### Your link

Split editing view on desktop: narrow settings left, live public preview right. Mobile toggles between Edit and Preview. Settings are only display name/photo, handle, pricing mode, base price/durations, optional identity link and pause/publication state.

Availability, meeting delivery and payout readiness appear as clear linked sections rather than turning the main editor into a long form.

Preview uses the real public component with draft data. Saving must persist to the backend, invalidate relevant caches and update the OpenGraph version. Existing bookings preserve snapshots.

### Money

This is **not a wallet**. No “top up,” “send money,” “withdraw” or spendable balance.

Show:

- Paid bookings and gross amount for a date range.
- Seller entitlement after the disclosed deduction.
- Amount awaiting provider settlement.
- Amount provider-confirmed settled/sent.
- Failed or exceptional settlement items.

Explain “awaiting settlement” in ordinary language. A payment success cannot appear as bank-paid. Detail pages show the related booking, fee snapshot, provider route, destination masked to bank/last four digits, and reference.

CSV export must be authorized, rate-limited and protected against spreadsheet-formula injection. Do not expose raw payout account details in exports.

### Settings

Account identity, verified email, timezone, notification preferences, active sessions, payout destination and privacy/deletion request. Email or bank changes require reauthentication and create an audit event. Security notifications are mandatory; promotional communications are optional and default off.

A seller who pauses or closes their link still needs access to outstanding bookings, statements and obligations.

---

## 16 — Availability and meeting delivery

### Availability controls

Weekly hours, date overrides, time off, timezone, enabled durations, minimum notice, booking horizon and buffer. Keep advanced controls collapsed.

Proposed beta defaults: **2 hours minimum notice**, **30-day booking horizon**, **10-minute post-call buffer**, and **15/30/60-minute options** with 30 enabled by default. These are editable product defaults, not claims about user preferences. A seller must explicitly publish a schedule.

Multiple availability windows per day are allowed. Overrides can close a date or replace its windows. Existing bookings survive schedule edits. Server availability must intersect schedule, notice, horizon, buffers, active holds, confirmed bookings and relevant calendar blocks.

### Manual schedule first; calendar connection optional

V1 does not need Google OAuth just to reserve time. Explain that without a connected calendar, only bookings made through this product and manually entered blocks are checked. Do not imply protection against events on an unconnected calendar.

An optional calendar connection may later read busy periods and create a unique meeting event. Request the minimum scopes actually needed. Treat disconnection and provider downtime explicitly. Google event/conference creation is covered in reference C1; do not fabricate meeting links.

### Meeting delivery: beta baseline

Use external meetings, not built-in video. Baseline mode is **a private, booking-specific HTTPS meeting link supplied by the seller**. Allow approved providers such as Google Meet and Zoom after URL validation. Do not expose a reusable personal room publicly.

After payment, the seller receives a prominent **Add meeting link** action. The buyer sees: **Your booking is confirmed. Tomi will add the meeting link before the call. We’ll email it to you.** State the actual deadline, initially 30 minutes before the start. Remind the seller well before that deadline and surface overdue delivery to operations.

A seller must choose and accept this delivery method before publication. Missing links are an operational exception, not a reason to lie about delivery. If manual link entry proves unreliable, prioritize proper automatic meeting provisioning before adding growth features.

Optional connected-calendar mode creates a unique event/meeting per booking. Meeting creation retries use a stable request identifier. A provider outage does not erase payment or duplicate the event.

### Security and privacy

Meeting URLs are private, encrypted where appropriate and visible only to participants and specifically authorized support personnel. Do not put private join URLs in public OpenGraph images, referral parameters, public profile JSON or broad analytics.

Calendar invitations should not expose other customers. Each booking has only its own participants. No automatic recording or transcription. Joining a meeting should not require installing this product’s app.

---

## 17 — Sharing and the growth experience

### Link first

A persistent copy action appears after readiness, on overview and in the share page. Use the Web Share API when supported, with copy fallback. WhatsApp and X sharing are explicit user actions. Never access address books or send invitations automatically.

Measure a copy click as **copy clicked**, not “link sent.” The application cannot know whether a copied link was delivered in a private chat.

### Share text

Default:

> Want to talk? Pick a time here: {public_url}

Optional lighter version:

> “Can I pick your brain?” Yes. Here’s my link: {public_url}

Do not force “my brain is for rent” or jokes about greed into everyone’s professional identity. Give two tasteful copy choices, not a meme generator.

### OpenGraph preview

Generate a `1200 × 630` image using the same typography and colors. It contains avatar/initials, name, “A little time with {first_name},” and either a static price/duration or “Make an offer.” Small brand mark in the lower corner.

Do not put “available now,” live slot counts or rapidly changing timestamps in a social preview; external platforms cache it. The page itself provides current availability.

Public image rendering must use sanitized text and owned/validated media. Never fetch arbitrary user-provided URLs in the renderer. Limit text length and rendering cost; cache by profile/pricing/image version.

### Share cards

Offer a square `1080 × 1080` card and portrait `1080 × 1920` card. Name, optional photo, short line and personal link. Do not reveal buyer identities, booking notes or transaction amounts by default. A first-payment celebration can say **My first paid booking** without publishing earnings.

Use approved font assets server-side; the downloadable card includes the resulting image, not font files. No download should be required to copy the link.

### Buyer-to-seller prompts

First surface: beneath a successful booking confirmation. Second optional surface: the private page after the conversation’s time window, subject to frequency caps. If the person already has a link, show a quiet shortcut rather than signup copy.

Do not interrupt payment, obscure the meeting URL, precheck marketing consent or silently create a public profile. A buyer clicking “Get my own link” reuses verified identity only where lawful and technically proven; they still choose their handle and pricing.

### Retention prompt

After the seller’s first real payment, show a restrained success message with **Share your link**. After actual settlement, the message can say that the money was sent. Keep those events separate.

### What not to build

No fabricated social proof, celebrity accounts claimed without consent, fake “offers waiting,” countdown to an invented handle deadline, public earnings leaderboard, compulsory referrals, mass DMs, auto-posting, contact imports or incentives that manufacture circular payments.

---

## 18 — Operations admin

### Intent

Build a small operations console for a real paid product. It is not a customer-satisfaction court. Its job is to see what happened, identify objective failures, protect accounts, reconcile money and measure adoption.

No arbitrary “mark paid” button. No direct editing of balances. No deleting payment history. No hidden impersonation feature.

### Admin shell

Same typography and design discipline as the product, with a denser neutral surface. A narrow left rail and a useful top search. Search accepts exact internal/public IDs, payment reference, verified account identifier and handle. Mask sensitive values and apply permissions.

Status colors remain consistent with the customer app. Avoid a wall of charts. The first page should tell the operator what needs action now.

### Overview

Operational queues first: payment verification exceptions, overdue meeting links, failed settlements, provider dispute deadlines, webhook processing lag, failed email delivery and restricted accounts.

Secondary summaries: real paid bookings, gross amount, seller entitlement, gross platform fee allocation and actual processor cost. Show date range, currency and timezone. Never call GMV “revenue.” Never display mock growth in production.

### People

List with handle, account status, seller readiness, joined date and verified contact state. Detail includes profile history, payout onboarding status, masked destination, bookings, payments and audit events.

Permitted actions: pause new bookings, restrict an account with a reason, revoke sessions, initiate an approved recovery process and view authorized data. Suspension does not erase existing obligations or automatically confiscate earned money.

### Bookings

Show participant identities at appropriate permission level, immutable terms, slot history, payment linkage, meeting delivery and completion evidence. The timeline distinguishes application events from provider-confirmed events and participant reports.

Operations may coordinate an objective issue or an approved correction; it cannot secretly change the agreed price or meeting time.

### Payments

Detail layout:

1. Header: amount, currency, canonical state, provider reference and environment.
2. Quote/booking association and immutable seller snapshot.
3. Allocation: seller entitlement, total deduction, processor fees, platform net contribution where known.
4. Collection timeline and provider verification evidence.
5. Settlement route and destination snapshot.
6. Exceptions, audit and safe operational actions.

“Recheck with provider” queues an idempotent verification task. Replaying a webhook cannot create a second booking, ledger entry or payout. Hide raw provider payloads from normal support roles.

### Settlements

Queue by route and state. Distinguish direct provider settlement from an explicitly approved expedited transfer. Show aging, expected date where known, provider-confirmed result and reconciliation coverage.

Do not offer “Retry payout” on an indeterminate transfer. Resolve its status first. Display a clear warning when the selected route cannot be stopped by the application because the provider is already settling it.

### Exceptions

Types: duplicate charge, late successful payment without a valid slot, seller cancellation/non-delivery, unauthorized-payment/provider dispute, failed or reversed transfer, amount/currency mismatch, identity/payout mismatch and unresolved reconciliation.

Detail: objective facts, provider deadline if supplied, evidence, assigned operator, audit and permitted next actions. No star ratings, subjective advice-quality scoring or “make customer happy” refund button.

### Growth

See Section 30. Show the actual buyer → seller → first paid booking funnel, cohort age, attribution coverage and a privacy-preserving lineage view. Access is restricted. This graph is an internal measurement tool, not a public social network.

### System

Payment-provider health, last successful reconciliation, unprocessed webhooks, job lag, email status, storage failures and feature flags. A feature flag must not bypass financial safety controls.

### Audit

Append-only records for sensitive reads and all material writes. Search by actor, target, action and time. Restrict exports. Application administrators cannot erase the audit record of their own action.

### Roles

| Role | Typical powers | Explicit restrictions |
|---|---|---|
| Owner | Configuration and access approval; commercial settings within the fee ceiling. | No deletion of ledger/audit history; sensitive changes require reauthentication. |
| Operations | Booking issues, user restrictions, permitted communication. | No bank-account editing, arbitrary transfers or fee changes. |
| Finance | Reconciliation, provider evidence, approved monetary exception actions. | No unlogged account takeover or payout-destination changes. |
| Analyst | Aggregated growth and revenue measures. | No raw notes, contact details, bank details or operational writes. |
| Read-only support | Minimum booking/account information needed for support. | No financial mutation or raw provider payload access. |

Only Owner is needed for the first deployment, but permission checks must be real from day one. Require MFA for operations access. Role assignment is not a field a user can update on their own account.

---

## 19 — Screen states and finished-product details

Every important page must handle loading, empty, error, unavailable, permission denied, success and small-screen layouts. Add the states during implementation, not after the homepage is “done.”

### Specific failure copy

| Situation | Copy direction |
|---|---|
| Slot taken | That time was just booked. Choose another one. |
| Hold expired before payment starts | Your time hold ended. Choose a time again. |
| Payment unresolved | We’re still checking this payment. Please don’t pay again yet. |
| Confirmed decline | The payment didn’t go through. You can try again. |
| Offer expired | This offer has expired. You can send a new one. |
| Counteroffer accepted elsewhere | This offer has changed. Refresh to see the latest reply. |
| Link not payment-ready | This page isn’t taking bookings yet. |
| Payout pending | Payment received. Your bank transfer is not complete yet. |
| Provider settlement delayed | Your payment is recorded. Settlement is taking longer than expected. |
| Email delivery issue | We couldn’t deliver that email. Check the address or use secure booking access. |
| Missing meeting link | The meeting link hasn’t been added yet. We’ve alerted {name}. |
| Forbidden record | You don’t have access to this booking. |
| Generic failure | Something went wrong. Try again. Reference: {safe_request_id}. |

Never show raw SQL, stack traces, tokens or provider secrets. Never say “no money left your account” unless that is actually established.

### Detail checklist

Favicon, browser title, touch icon, proper selected states, visible keyboard focus, sensible autofill, correct mobile input keyboards, contextual page headings, no layout shift during payment verification, copy confirmation, focus restoration, disabled-state explanations, error summary for long forms, consistent currency formatting, humane empty screens and functioning browser back navigation.

## 20 — Technology and architecture

### Recommended stack

Use a **SvelteKit + Go modular monolith**, with PostgreSQL as the source of truth. This keeps the web experience fast and the money/scheduling rules in one backend. It also avoids introducing an unnecessary new stack alongside the founder’s existing engineering work.

| Area | Choice |
|---|---|
| Web | Svelte 5, SvelteKit, strict TypeScript, server-rendered public pages. |
| Styling | Tailwind CSS with semantic CSS variables; custom-designed components. |
| Accessible primitives | shadcn-svelte/Bits UI where useful, fully restyled. |
| Icons | One consistent, permissively licensed SVG icon set; no mixed icon families. |
| Backend | Go, supported stable toolchain, standard library plus a small router such as Chi. |
| Database | Supported PostgreSQL version with `btree_gist`; SQL migrations. |
| Database access | `pgx` and `sqlc`; explicit, reviewable SQL. |
| API | Versioned REST JSON and OpenAPI; generated TypeScript contracts. |
| Jobs | PostgreSQL-backed durable outbox/job workers. No Redis requirement in V1. |
| Media | Private S3-compatible object storage plus controlled public derivatives. |
| Payments | Paystack adapter initially, subject to account/product approval. |
| Email | A verified transactional email provider behind an adapter. |
| Meeting | Private per-booking external link; optional calendar provider adapter later. |
| Deployment | Containers behind a same-origin reverse proxy; separate web/API/worker processes. |
| Observability | Structured logs, request IDs, metrics, error reporting and audit events. |

Resolve current stable compatible package versions at implementation time. Pin exact versions/lockfiles and container versions; do not use `latest` in production. Verify the current Svelte/Tailwind/shadcn setup using references F1–F3 rather than mixing outdated setup instructions.

### Keep the boundaries clear

SvelteKit owns presentation and SSR. Go owns identity authorization, prices, availability, offers, bookings, payments, ledger, settlement state, notification decisions and admin permissions. Do not duplicate business rules in TypeScript and Go with independent behavior.

Use one external origin:

```text
Browser
  -> reverse proxy
       /api/v1/* -> Go API
       everything else -> SvelteKit

Go API -> PostgreSQL
Go worker -> PostgreSQL / Paystack / email / object storage
SvelteKit SSR -> private Go API endpoint with the user's scoped session
```

If SvelteKit form actions proxy a request, they must preserve the relevant authenticated context and CSRF protection. Go still authorizes the resource. Do not trust an arbitrary `X-User-Id` supplied by the browser. Public server rendering and progressive enhancement should follow current SvelteKit guidance; reference F4.

### Proposed repository structure

```text
/
├── README.md
├── AGENTS.md
├── Makefile
├── compose.yaml
├── .env.example
├── .gitignore
├── apps/
│   └── web/
│       ├── package.json
│       ├── pnpm-lock.yaml
│       ├── svelte.config.js
│       ├── vite.config.ts
│       ├── src/
│       │   ├── app.html
│       │   ├── hooks.server.ts
│       │   ├── lib/
│       │   │   ├── api/              # generated contracts + thin client
│       │   │   ├── brand/            # name, domain, wordmark configuration
│       │   │   ├── components/
│       │   │   │   ├── ui/
│       │   │   │   ├── public/
│       │   │   │   ├── booking/
│       │   │   │   ├── app/
│       │   │   │   └── ops/
│       │   │   ├── copy/
│       │   │   ├── formatting/
│       │   │   ├── server/
│       │   │   └── styles/
│       │   └── routes/
│       │       ├── (marketing)/
│       │       ├── (auth)/
│       │       ├── (public-person)/[handle=handle]/
│       │       ├── booking/[public_id]/
│       │       ├── offer/[public_id]/
│       │       ├── app/
│       │       └── ops/
│       └── static/
├── services/
│   └── core/
│       ├── go.mod
│       ├── go.sum
│       ├── cmd/
│       │   ├── api/
│       │   ├── worker/
│       │   └── admin-bootstrap/
│       ├── internal/
│       │   ├── config/
│       │   ├── httpapi/
│       │   ├── auth/
│       │   ├── people/
│       │   ├── pricing/
│       │   ├── availability/
│       │   ├── offers/
│       │   ├── bookings/
│       │   ├── payments/
│       │   ├── settlements/
│       │   ├── ledger/
│       │   ├── meetings/
│       │   ├── notifications/
│       │   ├── growth/
│       │   ├── operations/
│       │   ├── audit/
│       │   ├── jobs/
│       │   ├── adapters/
│       │   │   ├── paystack/
│       │   │   ├── email/
│       │   │   ├── storage/
│       │   │   └── calendar/
│       │   └── database/
│       ├── migrations/
│       ├── queries/
│       └── sqlc.yaml
├── api/
│   └── openapi.yaml
├── infrastructure/
│   ├── containers/
│   ├── proxy/
│   └── deployment/
├── scripts/
│   ├── seed-demo/
│   ├── verify-local/
│   └── reconcile/
└── docs/
    ├── IMPLEMENTATION_STATUS.md
    ├── DECISIONS.md
    ├── DESIGN_SYSTEM.md
    ├── STATE_MACHINES.md
    ├── PROVIDER_CAPABILITIES.md
    ├── MONEY_FLOW.md
    ├── DATA_DICTIONARY.md
    ├── SECURITY.md
    ├── OPERATIONS.md
    ├── LAUNCH_CHECKLIST.md
    └── screenshots/
```

Generate only meaningful layers. A handler calls a domain service, which performs authorized work using explicit queries and adapters. Avoid interfaces with one trivial implementation everywhere; introduce boundaries where provider substitution or domain separation matters.

Do not create microservices, Kubernetes, a separate analytics warehouse or a distributed event bus for the first release.

---

## 21 — Data model

Use internal UUIDs, separate public identifiers and explicit timestamps. Store instants as `timestamptz`; store a person’s chosen IANA timezone separately. Store money as integer minor units with a currency code. Never use floating-point money.

For JSON, transmit minor-unit values as decimal **strings**, so future values cannot exceed JavaScript’s safe integer range unnoticed. In Go, use checked integer arithmetic. Enforce realistic provider/account bounds before multiplication.

### Identity and person data

| Entity | Important fields and rules |
|---|---|
| `users` | ID, display name, status, timezone, created/updated timestamps. No user-editable admin role. |
| `user_identities` | User ID, type, normalized identifier, verified timestamp; unique ownership by approved identity rules. |
| `sessions` | Hash of random token, user/guest binding, scope, expiry, revocation and last-seen data. |
| `email_challenges` | Challenge ID, keyed code hash, purpose, expiry, attempts, consumed timestamp. |
| `admin_grants` | Explicit permissions, granted by, grant/revoke timestamps; audited. |
| `seller_profiles` | User ID unique, normalized handle unique, mode, publication state, readiness state, avatar derivative, optional identity link. |
| `handle_aliases` | Old handle, original owner, canonical handle, retention state; prevent alias takeover. |
| `pricing_versions` | Seller, version, currency, base-30 amount, enabled durations, fee-policy version reference, effective date. Immutable once used. |
| `policy_acceptances` | Actor identity, policy version/hash, time and contextual record. |

Email normalization must not collapse unrelated addresses by removing dots or plus tags. Verify actual ownership before linking records. A payout account match alone is not permission to merge users.

### Scheduling and transactions

| Entity | Important fields and rules |
|---|---|
| `availability_windows` | Seller, weekday, local start/end times, timezone and active rule version. |
| `availability_overrides` | Seller, local date, closed flag or replacement windows. |
| `calendar_connections` | Optional provider, encrypted tokens, scopes, status; do not build unused provider complexity. |
| `busy_intervals` | Seller, source, external reference, UTC range; unique source event identity. |
| `guest_contacts` | Verified/unverified email identity; minimum necessary name; no implicit public account. |
| `offers` | Seller, guest/user sender, duration, current amount/version, state, expiry, optional note. |
| `offer_versions` | Immutable proposal/counteroffer terms and actor; preserve accepted version. |
| `booking_quotes` | Seller, guest scope, source pricing/offer version, duration, slot, gross, fee snapshot, currency, expiry and terms hash. |
| `slot_reservations` | Seller, occupied UTC interval including buffer, kind, active flag, expiry where applicable, quote/booking association. |
| `bookings` | Participants, immutable quote, confirmed schedule, reservation, booking state, delivery state, completion-evidence state. |
| `reschedule_requests` | Booking, proposed time, requester, state/version, response and history. |
| `meeting_details` | Booking unique, provider, encrypted private URL, delivery deadline and ready timestamp. |
| `payment_attempts` | Quote, provider, environment, unique merchant reference, provider transaction ID, expected/actual amounts, currency, canonical state. |
| `payment_allocations` | One economic allocation per successful payable booking; seller entitlement, gross fee, actual processor cost, policy/routing snapshots. |

Notes are private data. They are not profile content or analytics dimensions. Never put raw notes in provider metadata.

### Money, provider and operational records

| Entity | Important fields and rules |
|---|---|
| `payout_destinations` | Seller, immutable destination version, provider recipient/subaccount reference, masked bank details, verification state. |
| `seller_onboarding_checks` | Provider-required readiness result and reference; minimize sensitive identity storage. |
| `settlement_items` | Payment allocation, exactly one route, destination snapshot, expected amount, provider reference and state. |
| `settlement_confirmations` | Provider evidence/import/API result, amount, time, source and reconciliation state. |
| `provider_events` | Provider/environment, event identity or payload digest, raw-body hash, verified receipt time, processing state. |
| `ledger_accounts` | Account type, currency, owner/control scope. |
| `journal_entries` | Immutable economic event, source reference, currency, posted time and optional reversal link. |
| `journal_postings` | Journal, account, debit/credit and minor-unit amount; journals balance. |
| `payment_exceptions` | Objective type, linked entities, provider deadline, state, permitted action history. |
| `outbox_jobs` | Type, dedupe key, payload reference, run-at, lease, attempts, retry state. |
| `notification_deliveries` | Event, channel, recipient identity, template version, provider ID and delivery result. |
| `product_events` | Allowlisted event fields, pseudonymous identity where possible, event/received times and environment. |
| `growth_relationships` | Verified source/child relation, qualifying payment, window and attribution method. |
| `audit_events` | Actor, permission context, action, target, reason, safe before/after summary, timestamp and request ID. |

Use separate transaction-state histories where necessary rather than repeatedly overwriting the only evidence. Do not persist every transient frontend state as a new table.

### Required constraints

- Unique normalized handle, including protected alias rules.
- Unique provider transaction ID within provider/environment.
- Unique merchant payment reference within provider/environment.
- At most one converted booking for one agreed offer.
- At most one normal economic allocation per paid booking; duplicate charges go to exceptions, not seller revenue.
- Exactly one settlement route per allocation, immutable after collection initialization.
- Unique payout/settlement idempotency identity.
- Positive durations and amounts, supported currency, bounded fee basis points.
- Balanced journal entries and immutable posted journals.
- Foreign keys and deletion behavior appropriate to financial retention.
- Real overlap prevention for each seller’s active reservations.

---

## 22 — Scheduling correctness and concurrency

### Time rules

Build availability on the backend. Convert local schedule rules to actual UTC instants for the requested date window. Handle daylight-saving transitions for users outside Nigeria. Do not create nonexistent local times; disambiguate repeated times visibly. A fixed offset is not a timezone.

Use half-open occupied intervals: `[start, end + buffer)`. Back-to-back availability follows that definition. Recalculate availability after schedule edits, but never rewrite confirmed booking times silently.

### Overlap protection

PostgreSQL range exclusion constraints are suitable for preventing overlaps; see reference D1. An illustrative schema pattern:

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE slot_reservations (
    id uuid PRIMARY KEY,
    seller_id uuid NOT NULL REFERENCES seller_profiles(id),
    occupied_from timestamptz NOT NULL,
    occupied_to timestamptz NOT NULL,
    occupied_range tstzrange GENERATED ALWAYS AS (
        tstzrange(occupied_from, occupied_to, '[)')
    ) STORED,
    active boolean NOT NULL DEFAULT true,
    reservation_kind text NOT NULL
        CHECK (reservation_kind IN ('hold', 'booking')),
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (occupied_to > occupied_from),
    CHECK (reservation_kind <> 'hold' OR expires_at IS NOT NULL),
    EXCLUDE USING gist (
        seller_id WITH =,
        occupied_range WITH &&
    ) WHERE (active)
);
```

Adapt associations and indexes to the final schema. The key invariant is database-enforced exclusivity, not this exact table shape.

**Important:** do not put `expires_at > now()` into a partial index and assume the index expires holds by itself. Explicitly deactivate expired holds in a transaction and run a cleanup worker. Reservation creation should safely clear relevant expired holds before inserting the new one.

### Holds

Start with a server-configured hold period, such as 10 minutes, but align it with the enabled provider channel’s documented checkout/transfer timing. Show the actual expiry only if useful. No fake urgency timer.

Do not extend a hold indefinitely because a tab remains open. Do not release a confirmed booking when a cleanup job sees an old quote expiry. A late provider event must take the explicit late-payment path.

### Concurrent actions

Use database transactions and row versions/locks for offer responses, quote consumption, payment allocation, rescheduling and payout initiation. If two buyers attempt one slot, one succeeds; the other receives `SLOT_UNAVAILABLE`. If two payments succeed against one intended booking, the second becomes a duplicate-charge exception rather than a second payout.

Keep external HTTP calls outside long database locks. Use durable intents/outbox records and stable references to recover from network uncertainty.

### Separate state machines

```text
Booking:
  HELD -> CONFIRMED -> ELAPSED
  HELD -> EXPIRED
  CONFIRMED -> CANCELED
  any relevant state -> NEEDS_ATTENTION (with preserved prior facts)

Payment attempt:
  CREATED -> INITIALIZING -> PENDING -> SUCCEEDED
                                  -> FAILED
                                  -> ABANDONED (only on reliable evidence)
  SUCCEEDED -> REVERSED / PARTIALLY_REVERSED (with recorded provider event)

Meeting delivery:
  REQUIRED -> READY
  REQUIRED -> OVERDUE
  READY -> REPLACED (retain history)

Completion evidence:
  UNKNOWN -> TIME_ELAPSED
          -> ONE_PARTICIPANT_CONFIRMED
          -> BOTH_PARTICIPANTS_CONFIRMED
          -> PROVIDER_CONFIRMED (only with real supported evidence)

Settlement:
  ALLOCATED -> SCHEDULED -> PROVIDER_PENDING -> SENT
  ALLOCATED -> HELD_FOR_OBJECTIVE_EXCEPTION
  PROVIDER_PENDING -> FAILED / REVERSED
```

A provider dispute can coexist with an otherwise successful payment. Store the dispute separately rather than losing the fact that collection occurred. Likewise, a canceled booking does not mean money automatically reversed.

---

## 23 — Backend service responsibilities

### Identity

Verify email, issue/revoke scoped sessions, claim handles, enforce roles, handle recovery and identity linking. Authorization is server-side and resource-specific.

### Pricing

Calculate fixed-duration amounts with deterministic integer rounding. Version public pricing. Validate fee feasibility and approved limits. Produce immutable quotes. A changed profile price never changes an existing agreed offer or payment in flight.

### Availability

Expand local schedule rules, apply overrides and busy intervals, create/expire holds, enforce exclusivity and support atomic rescheduling.

### Offers

Own the negotiation state machine and version checks. Validate actor, state, amount, duration, expiry and conversion uniqueness.

### Bookings

Coordinate slot and quote lifecycle, produce participant-specific views, manage delivery and schedule changes, and record completion evidence without inventing it.

### Payments

Initialize provider checkout, verify collection, process authenticated events, match expected/actual values and coordinate economic allocation. Never accept a frontend “success” flag as payment evidence.

### Settlements

Track the approved route and immutable destination. Reconcile provider movement. Implement transfer initiation only when that route is explicitly approved and funded. Never run direct split and an additional payout for the same seller entitlement.

### Ledger

Append balanced economic events and reversals. Provide derived totals; no manually editable balances. Keep operational allocation accounting distinct from claims about statutory financial reporting.

### Notifications

Render versioned templates from real domain events. Queue delivery reliably, suppress duplicates and update delivery state. Respect consent and preferences while retaining mandatory security/transaction communications.

### Growth

Record defined first-party events, exclude synthetic/internal transactions and produce cohort measures with clear attribution limits.

### Operations

Expose permission-gated views and constrained actions. All sensitive operations go through services, not direct database edits from an admin UI.

---

## 24 — API contract

Create OpenAPI definitions before wiring complex screens. Generate frontend request/response types. Use `application/json`, stable error codes, opaque IDs and cursor pagination where appropriate.

### Response examples

```json
{
  "data": {
    "id": "quote_opaque_id",
    "currency": "NGN",
    "gross_minor": "1000000",
    "deduction_minor": "50000",
    "seller_entitlement_minor": "950000",
    "duration_minutes": 30,
    "starts_at": "2026-10-02T15:00:00Z",
    "viewer_timezone": "Africa/Lagos",
    "expires_at": "2026-10-02T13:10:00Z"
  },
  "request_id": "req_opaque_id"
}
```

Dates above are illustrative, not seeded live availability.

```json
{
  "error": {
    "code": "SLOT_UNAVAILABLE",
    "message": "That time was just booked. Choose another one.",
    "fields": {},
    "retryable": false
  },
  "request_id": "req_opaque_id"
}
```

### Minimum endpoint groups

| Method and path | Purpose |
|---|---|
| `POST /api/v1/auth/challenges` | Begin a purpose-bound email verification. |
| `POST /api/v1/auth/challenges/{id}/verify` | Verify code; issue appropriate scope. |
| `POST /api/v1/auth/logout` | Revoke current session. |
| `GET /api/v1/me` | Current identity, capabilities and readiness. |
| `GET /api/v1/handles/{handle}/availability` | Rate-limited handle availability. |
| `POST /api/v1/me/link` | Claim seller profile/handle explicitly. |
| `PATCH /api/v1/me/link` | Versioned profile/pricing/publication updates. |
| `GET /api/v1/people/{handle}` | Sanitized public profile. |
| `GET /api/v1/people/{handle}/slots` | Bounded date-window availability. |
| `POST /api/v1/quotes` | Create scoped quote and slot hold. |
| `GET /api/v1/quotes/{id}` | Authorized quote status. |
| `POST /api/v1/quotes/{id}/checkout` | Initialize/resume one payment attempt. |
| `GET /api/v1/checkouts/{id}` | Authorized payment status; no sensitive provider data. |
| `POST /api/v1/offers` | Submit a verified guest/user offer. |
| `GET /api/v1/offers/{id}` | Authorized offer detail. |
| `POST /api/v1/offers/{id}/accept` | Accept the current proposal version. |
| `POST /api/v1/offers/{id}/counter` | One seller counteroffer. |
| `POST /api/v1/offers/{id}/decline` | Decline under state rules. |
| `POST /api/v1/offers/{id}/withdraw` | Withdraw only when safe/permitted. |
| `GET /api/v1/me/bookings` | Buyer/seller filtered view. |
| `GET /api/v1/bookings/{id}` | Participant-scoped booking. |
| `POST /api/v1/bookings/{id}/reschedules` | Propose a new slot. |
| `POST /api/v1/reschedules/{id}/accept` | Atomically move reservation. |
| `POST /api/v1/reschedules/{id}/decline` | Retain original booking. |
| `PUT /api/v1/bookings/{id}/meeting` | Seller adds/replaces authorized private link. |
| `POST /api/v1/bookings/{id}/completion` | Record participant confirmation, not a payout approval. |
| `POST /api/v1/bookings/{id}/issues` | Report a defined objective issue. |
| `GET/PUT /api/v1/me/availability` | Read/versioned schedule updates. |
| `GET /api/v1/me/earnings` | Ledger-derived seller report. |
| `GET /api/v1/me/settlements` | Real settlement status. |
| `POST /api/v1/me/payout-onboarding` | Start approved provider readiness flow. |
| `POST /api/v1/me/payout-destinations` | Verified, reauthenticated destination change request. |
| `POST /api/v1/access/challenges` | Safe cross-device guest access. |
| `POST /api/v1/events` | Allowlisted, rate-limited client analytics only. |
| `POST /api/v1/webhooks/paystack` | Signature-authenticated provider inbox. |
| `/api/v1/ops/*` | Permission-scoped operations endpoints. |

Define operation-specific admin endpoints, not a generic “update any record” API. Use secure upload initiation/completion endpoints for avatars; never allow arbitrary file paths.

### Idempotency

Require `Idempotency-Key` on quote checkout, offer actions, settlement initiation and other retry-sensitive mutations. Scope keys by actor, route and operation. Persist the request-body digest and the canonical response. Same key with a different body returns a conflict.

Provider references and economic event uniqueness remain durable beyond ordinary API key-retention windows. A payment cannot be paid out twice simply because an HTTP idempotency record expired.

### Security and errors

Use `401` for missing authentication, `403` for denied permitted knowledge where appropriate, `404` to avoid revealing private record existence, `409` for stale/conflicting state, `422` for validation and `429` for throttling. Include `Retry-After` when useful. Never expose provider secrets through error mapping.

All listing endpoints need bounded page size and queries. All availability endpoints need bounded date ranges. All export endpoints need access checks and audit.

---

## 25 — Authentication, authorization and security

### Sessions

Use high-entropy opaque session tokens with server-side records and hashed storage. Cookies are secure, HTTP-only, host-scoped and appropriately SameSite. Use separate scopes for authenticated users, restricted guests and privileged operations. Do not store bearer credentials in localStorage.

Current session and server-hook guidance is in F5; OWASP’s session guidance is S1. This specification intentionally chooses revocable server-side sessions, not long-lived browser JWTs.

### Email challenges

Use cryptographically generated codes, a keyed hash/pepper rather than a plain hash of a short code, purpose binding, a short expiry and limited attempts. A possible starting policy is eight digits, 10-minute lifetime and five attempts, with per-address/IP/device throttling. Tune based on abuse and delivery—not vanity conversion.

Verification and consumption must be atomic. Resend does not make all previous codes valid indefinitely. Do not reveal whether an email has an account through inconsistent responses.

### Guest isolation

An entered email does not authorize access to previous bookings. A successful payment does not prove ownership of that email. Account linking, cross-device history and verified growth identity require an actual verification step.

A restricted guest session should access only the intended checkout/booking/offer scope. Creating a seller profile requires explicit user intent and verified identity.

### CSRF and request trust

Protect all cookie-authenticated state-changing requests using Origin checks and a session-bound CSRF mechanism. Check the framework’s actual protection for each request type; do not assume Svelte form protection covers every Go JSON endpoint. Webhooks use their own provider signature mechanism, not browser CSRF tokens.

Configure trusted reverse proxies explicitly. Do not trust arbitrary forwarded headers. Restrict CORS to approved origins; do not combine wildcard origins with credentials.

### Operations access

MFA is mandatory. Provision the first owner through a controlled CLI/bootstrap procedure, not a hidden public form or a hardcoded password. Reauthentication is required for role grants, payout changes, exceptional monetary actions and production payment configuration.

### Destination changes

Changing a bank destination creates a new verified destination version and, where supported, a new provider routing object. Do not mutate a provider subaccount already referenced by unsettled payments and assume the local snapshot alone freezes routing.

If the provider cannot preserve routing for existing transactions, pause the change until the affected settlement obligations are resolved under an approved procedure. Notify the old and new verified contact channels. Apply a risk-appropriate cooling-off rule to the changed destination; disclose it rather than defeating it with an “instant payout” promise.

### Input, media and secrets

Validate inputs at the API boundary and domain layer. Parameterize SQL. Escape plain-text user content. Disallow raw HTML. Validate meeting URLs against an allowlist and never fetch arbitrary destinations server-side. Keep avatar uploads size-limited, decode/re-encode accepted raster formats, remove metadata, reject SVG/script uploads, and serve through controlled storage paths.

Keep provider keys, OAuth tokens, encryption keys and credentials in secret storage. Encrypt sensitive fields with authenticated encryption and versioned keys. Do not commit `.env` files, credentials, real customer exports or provider payload dumps.

### Baseline protections

HTTPS, security headers, strict content policy appropriate to hosted checkout, clickjacking protection, safe referrer policy, rate limits, dependency patching, least-privilege database/storage credentials, input-size limits and secret redaction. Consider shared-device behavior and session revocation in account recovery.

No claim of “bank-level security,” “fully compliant” or certification unless actually earned and documented.

## 26 — Payments and the five-percent promise

### A commercial ambiguity the agent must not hide

The founder’s instruction is **charges no higher than 5%**. It has not been conclusively established whether that means a platform commission excluding processor fees or the seller’s entire ordinary deduction.

**This specification adopts the conservative proposed default: the seller’s total ordinary deduction is at most 5%, with processing costs absorbed from that share.** There is no additional buyer platform fee. This protects the promise rather than silently implementing “5% plus other charges.” The founder must approve this interpretation before live launch.

If a different fee-bearing arrangement is later approved, update the quote, payout math, public pricing and terms together. Never change it by editing copy alone. The platform’s own fee must still never exceed 500 basis points.

### Deterministic allocation

Let:

```text
G = gross amount paid, in minor units
b = approved deduction rate in basis points, 0 <= b <= 500
F = floor(G * b / 10,000)
S = G - F
C = actual payment processing cost
T = actual separately incurred payout cost, if any

Seller entitlement = S
Platform gross fee allocation = F
Platform contribution before other operating costs = F - C - T
```

Use checked integer arithmetic. Flooring the deduction ensures it does not exceed the promised percentage through rounding. Validate that `G = S + F` for every quote and allocation. Where taxes or withholding apply, obtain an approved treatment and disclose it accurately; do not relabel an ordinary platform surcharge as tax.

### Example

For a ₦10,000 payment at 5%:

```text
Buyer pays                        ₦10,000
Seller entitlement                 ₦9,500
Total ordinary seller deduction       ₦500
```

Paystack’s published Nigeria local price checked for this brief is 1.5% + ₦100, with the fixed ₦100 waived below ₦2,500 and a ₦2,000 local fee cap. On this illustrative ₦10,000 local transaction, that schedule gives ₦250 processing cost, leaving ₦250 from the ₦500 deduction before any other applicable costs. This is not ₦500 profit. Recheck account-specific fees, applicable tax treatment and enabled channels before live use. Reference P1.

Do not apply card pricing to every channel automatically. Do not assume a separate transfer fee applies to ordinary split settlement; price the route actually used.

### Fee feasibility

Store a versioned provider-fee schedule for quote validation and compare it with actual charged fees during reconciliation. Minimum/maximum transaction values and enabled channels require explicit live configuration.

Some low amounts or expensive channels may cost more than the allowed deduction. The backend must either reject that combination before collection, use an explicitly budgeted subsidy, or use an approved cheaper route. It must not silently take the deficit from the seller or charge the buyer after confirmation.

A higher minimum amount is not automatically the correct business decision; record it for founder approval. Fee thresholds can be discontinuous, so do not assume every amount above a working low amount is profitable.

### Provider onboarding

Confirm the merchant is approved for this model and that the intended submerchant/individual seller onboarding is permitted. An API that can create a subaccount does not by itself prove the business has completed all necessary checks.

Do not accept real payments for an unready seller and hope to solve identity/payout onboarding afterward. A claimed link can exist while collection remains disabled.

### Collection lifecycle

1. Validate seller readiness, quote, slot, pricing/offer version and fee feasibility.
2. Persist one payment intent with a stable unique merchant reference and immutable settlement route.
3. Initialize provider checkout from the backend using the expected amount and approved routing configuration.
4. Store the provider response safely; redirect only to a validated provider checkout destination.
5. Receive signed webhook and/or trigger backend verification on return.
6. Match provider result against amount, currency, reference, environment, seller/quote association and merchant.
7. Atomically record success, consume the quote, confirm/reacquire the slot if valid, create one allocation and write outbox events.
8. Handle duplicate, late or mismatched payments as explicit exceptions.
9. Reconcile collection and settlement independently.

Paystack verification and transaction initialization fields are documented in P3–P4. Do not log raw customer/payment payloads broadly or use client-side metadata as authority.

### Webhook implementation

Paystack documents an `x-paystack-signature` HMAC-SHA512 over the raw event payload using the secret key; verify it before processing. Reference P5.

Our processing design:

- Bound the body size and preserve the exact raw bytes.
- Compare the signature in constant time.
- Store a durable inbox record before acknowledging success.
- Use a documented provider event ID where genuinely available; otherwise use a payload digest for inbox deduplication.
- Enforce economic idempotency separately, because different payloads may describe the same payment.
- Acknowledge only after durable receipt; process business work asynchronously.
- If storage is unavailable, return a retryable server failure rather than acknowledging and losing the event.
- Handle duplicates, delayed delivery and out-of-order events.

Do not assume every event has a unique event ID merely because its underlying transaction has an ID. Do not accept a screenshot of a transfer as evidence of payment.

---

## 27 — Settlement, fast payouts and the operational ledger

### The distinction that must survive implementation

**Split allocation is not instant bank settlement.** Paystack’s standard Nigeria settlement FAQ describes the next working day. Its transfer facility is a separate operation requiring sufficient available balance. References P1 and P6.

The founder wants near-immediate payouts. Preserve that goal, but do not fake it in the UI or use unfunded transfers. The agent cannot solve a provider/commercial limitation by changing a status label.

### Route A — Direct provider split settlement: beta default

At collection initialization, route the seller’s share to the approved seller subaccount and the platform share to the platform, with processor charges borne according to the approved all-in fee policy. Paystack supports subaccount splits and flat transaction allocations; see P2 and P7.

For the proposed 5% all-in policy, the platform bears processor costs from its allocation. Confirm the provider configuration in sandbox and on the actual merchant arrangement.

There is **no additional transfer** for the seller’s share. The provider settles according to its schedule. Show “Payment received” immediately after verification, then “Awaiting settlement,” then provider-confirmed settlement status.

Use the documented settlement API/reporting capabilities to reconcile account and subaccount settlement details; reference P8. If granular proof is unavailable for a record, display “settlement confirmation pending,” not a fabricated bank-paid timestamp.

### Route B — Approved expedited transfer: disabled until approved

This is a later capability, not something to enable because an API endpoint exists. It requires a documented provider arrangement and, where necessary, approved prefunded liquidity and risk limits.

Before implementing live expedited payouts, document:

- Where spendable transfer balance comes from before the collection settles.
- Which party carries reversal/chargeback and liquidity risk.
- The approved seller onboarding and limits.
- Exact fees and the effect on the 5% ceiling.
- How the route avoids also paying through direct subaccount settlement.
- How failed, pending and reversed transfers are reconciled.

If the platform advances its own money, record that explicitly as a liquidity advance in the operational accounting. Do not call unsettled collection money an available transfer balance. Do not use other customers’ money to make an undocumented advance.

### Non-negotiable routing invariant

```text
For each payment allocation:

DIRECT_SPLIT_SETTLEMENT
             XOR
APPROVED_EXPEDITED_TRANSFER
```

Never both. Route is selected before collection and is immutable afterward except through a formally reconciled migration/exception procedure. A transfer worker refuses an allocation with an active direct split route. A timeout must not trigger a “backup payout” that duplicates an unknown original transfer.

### Transfer safety, when enabled

Persist the intent, amount, recipient snapshot and stable reference before the request. Enforce one seller payout obligation per allocation. Retry an uncertain transfer only under the provider’s documented reference/idempotency behavior and after checking its status. Transfer state is not inferred from HTTP success alone. Reference P6.

Do not mark “sent” on a queued/OTP-required transfer. Track provider-required approvals, failures and reversals. Do not disable provider security controls simply to remove an implementation obstacle.

### Operational ledger

Keep an append-only, balanced ledger for money control and reconciliation. It is not a customer wallet. The exact statutory accounting treatment and revenue-recognition policy require an accountant’s review of the final legal arrangement.

Illustrative control accounts:

```text
PROCESSOR_RECEIVABLE_CONTROL
SELLER_PAYABLE_CONTROL:{seller_id}
PLATFORM_FEE_ALLOCATION
PROCESSING_COST
PAYOUT_COST
OPERATING_BANK_CONTROL
APPROVED_PAYOUT_LIQUIDITY_CONTROL
REVERSAL_RECOVERY_CONTROL
```

For direct split settlement, an illustrative operational sequence is:

```text
Verified normal collection:
  Dr PROCESSOR_RECEIVABLE_CONTROL       G
  Cr SELLER_PAYABLE_CONTROL             S
  Cr PLATFORM_FEE_ALLOCATION            F

Actual processing cost:
  Dr PROCESSING_COST                    C
  Cr PROCESSOR_RECEIVABLE_CONTROL       C

Provider confirms seller settlement:
  Dr SELLER_PAYABLE_CONTROL             S
  Cr PROCESSOR_RECEIVABLE_CONTROL       S

Provider confirms platform settlement:
  Dr OPERATING_BANK_CONTROL            F - C
  Cr PROCESSOR_RECEIVABLE_CONTROL       F - C
```

These are control-ledger examples, not a claim that the platform legally owns all gross funds. Adapt to actual provider reporting, tax treatment and contractual agency arrangement. Post batches/partial settlements accurately; do not pretend every provider settlement equals one booking.

For an approved advance route, cash outflow occurs from approved prefunded liquidity before normal settlement replenishes it. It must have a separate accounting path. Do not shoehorn it into Route A entries.

### Ledger controls

Every journal has an immutable source identity. Debits equal credits in one currency. Reversals append compensating entries; they do not edit or delete the original. Journal posting and the corresponding domain transition occur in one database transaction.

Derived balance tables, if used, are rebuildable caches with integrity checks—not the sole record of money. An operator cannot type a new balance into the admin screen.

### Reconciliation

Reconcile application payments to provider collections, allocations to settlement items, and settlement items to provider confirmation/bank evidence available under the arrangement. Store when and how a conclusion was reached. Surface differences and unknowns, rather than adjusting totals silently.

A provider reversal after seller settlement may create a recoverable obligation or platform loss according to the approved contract. Do not invent an automatic bank-debit right. Do not hide negative economics by reducing a future seller payment without lawful authority and disclosed terms.

---

## 28 — The smallest necessary exception system

### Founder intent

The product is not responsible for deciding whether someone’s advice was intellectually valuable. Do not build a subjective refund marketplace or an open-ended customer-support empire.

### Reality that cannot be deleted from the code

Card/payment-provider disputes, duplicate charges, objective non-delivery and legally required remedies may still occur. Paystack documents dispute handling and refund-related provider processes; references P9–P10. A terms checkbox cannot make those events technically impossible.

Build a **small, restricted exception mechanism**, not a public satisfaction guarantee.

| Event | Required behavior |
|---|---|
| Buyer says delivered advice was disappointing | No automatic satisfaction refund. Show the agreed scope and an appropriate contact route. Preserve any overriding obligations. |
| Same intended booking charged twice | Block second seller allocation; investigate/return duplicate through the approved provider process. |
| Successful payment but no valid slot | Do not invent a booking. Offer an agreed alternative or an approved remedy; record the objective exception. |
| Seller cancels or objectively does not deliver | Preserve evidence; coordinate rescheduling or required remedy under the approved terms. |
| Buyer does not attend | Follow the disclosed terms and applicable obligations; do not let a UI rule claim to override law. |
| Unauthorized-payment/provider dispute | Record provider case and actual deadline, alert authorized staff, preserve evidence and process the provider outcome. |
| Transfer pending | Verify/reconcile; never pay a second time because the first is slow. |
| Transfer reversed | Record reversal and restore the correct obligation; controlled retry after definitive state. |
| Wrong amount/currency | Quarantine from normal booking/settlement allocation and reconcile. |

Do not hardcode a dispute-response deadline remembered from an earlier conversation. Use the actual provider case deadline and current agreement.

A remedy requires role checks, evidence, a reason, a linked case and an audit record. Initially, an authorized operator may complete some provider actions in the provider dashboard and record/reconcile them in the app. The application must show whether an action is merely requested or provider-confirmed.

Do not quietly label this architecture “non-custodial” or “not liable.” Those are legal/contractual conclusions, not consequences of using subaccounts.

---

## 29 — Notifications, receipts and calendar files

### Email first

Use email as the baseline transactional channel. WhatsApp sharing is user initiated. Automated WhatsApp notifications require a separate consented, approved provider integration and are not a V1 dependency.

Authenticate the sending domain with the provider’s required records. Verify delivery, bounce and complaint handling. Local development uses a local mail catcher; provider credentials must never be faked.

### Required events

| Event | Recipient | Message purpose |
|---|---|---|
| Sign-in code | Account/guest email | Verify identity. |
| New offer | Seller | Review amount and duration. |
| Offer accepted/countered/declined | Visitor | Explain status and next action. |
| Booking paid and confirmed | Both participants | Correct details, calendar access and meeting status. |
| Payment still unresolved | Relevant participant, only when necessary | Avoid duplicate payment. |
| Meeting link added/changed | Buyer | Deliver private joining information safely. |
| Meeting link approaching deadline | Seller | Add link before the call. |
| Booking reminder | Both | Upcoming time and current meeting link. |
| Reschedule agreed | Both | New time, updated calendar event. |
| Cancellation/objective issue | Relevant participants | Actual status and next step. |
| Settlement confirmed/failed | Seller | Real money status, not optimistic language. |
| Email/bank/security change | Affected verified contacts | Protect account access and payout identity. |
| Provider deadline/critical exception | Authorized operator | Action with true deadline. |

### Design

Emails use the same identity: warm surface, dark text, generous spacing and one primary action. Prefer an attractive typographic layout over a giant image banner. Include a plain-text alternative, readable dates/timezones and a real support contact. Receipt details stay above growth copy.

Never include raw access tokens in analytics or tracking pixels. Transactional security messages should not carry marketing tracking.

### Delivery correctness

Create notification jobs in the same transaction as the domain event. Assign a stable event/recipient/template dedupe key. Retry temporary failures with backoff and track permanent failure. Do not announce “email sent” merely because a job exists.

A mail failure does not roll back a verified payment. A private booking page remains accessible securely. Updating a meeting link invalidates obsolete reminder payloads.

### Calendar

Generate valid ICS downloads with stable UID, timestamps, escaped text and correct timezone/UTC representation. Reschedules increment sequence and preserve the UID; cancellations use the correct cancellation semantics. Calendar files contain only that booking’s participants and private join information where authorized.

Do not confuse downloading an ICS file with connecting a user’s live calendar or detecting their external conflicts.

---

## 30 — Growth measurement without invented virality

### The hypothesis

```text
Existing seller shares a link
  -> new buyer makes a genuine payment
  -> buyer explicitly creates a seller link
  -> new seller receives a genuine payment from another person
  -> that next buyer may do the same
```

There are two different successes: repeated use by an existing seller, and creation of a new transacting seller through the buyer experience. Measure them separately.

### Event catalogue

| Event | Source of truth | Important notes |
|---|---|---|
| `public_link_viewed` | Client/SSR-qualified event | Exclude known bots, previews, seller self-views and synthetic traffic. |
| `duration_selected` | Client | UX measure, not economic evidence. |
| `slot_selected` | Client | Not a reservation or booking. |
| `quote_created` | Server | Actual valid quote/hold. |
| `checkout_initialized` | Server/provider result | Not payment success. |
| `payment_succeeded` | Server verified | Unique economic payment identity; exclude duplicated attempts. |
| `booking_confirmed` | Server | Valid slot + terms + payment. |
| `offer_submitted` | Server | Verified sender only. |
| `offer_agreed` | Server | Still unpaid. |
| `conversation_window_elapsed` | Server | Not confirmed attendance. |
| `conversation_confirmed` | Server from evidence | Include evidence type. |
| `seller_cta_viewed` | Client | Actual rendered qualifying surface. |
| `seller_cta_clicked` | Client | Intent, not activation. |
| `seller_link_claimed` | Server | Verified person explicitly creates a link. |
| `seller_ready` | Server | Can accept real payments. |
| `link_copy_clicked` | Client | Cannot be called “sent.” |
| `share_action_opened` | Client | Cannot prove a social post was published. |
| `seller_first_payment` | Server | First qualifying genuine payment to that seller. |
| `seller_repeat_payment` | Server | Subsequent qualifying payment. |
| `settlement_confirmed` | Server/provider | Money status, not a growth proxy. |

Client analytics endpoints cannot write authoritative financial events. Analytics failure must never block a payment or booking.

### Identity and attribution

Use first-party, privacy-conscious attribution. A verified buyer identity can link their purchase and subsequent seller creation. Do not identify people by fingerprinting or a plain email supplied without verification. Do not infer that two people are the same because they share an IP address.

Track initial personal-link source, an optional non-PII share identifier, the qualifying purchase, CTA placement and account-linking evidence. Never put buyer email, booking note, amount or private booking ID into a public referral URL.

For a new seller with multiple prior purchases, use one documented attribution rule—for example, the first qualifying paid booking within the prior 30 days that led to a recorded seller CTA. Also retain broader unattributed conversion counts. Do not assign every later buyer to every possible ancestor.

Unknown attribution is a valid result. Report coverage. Private messages and cross-device behavior can leave gaps.

### Primary cohort measures

For unique verified buyers who were not already sellers at their qualifying purchase:

```text
Buyer -> claimed link rate
  = buyers who claim a link within the observation window
    / eligible unique buyers

Buyer -> ready seller rate
  = buyers who become payment-ready within the window
    / eligible unique buyers

Buyer -> first paid booking rate
  = buyers who receive a first qualifying payment as a seller within the window
    / eligible unique buyers
```

Show 7-, 14- and 30-day windows using matured cohorts. A buyer who arrived yesterday must not be counted as a 30-day failure today. Display cohort size, age and exclusions.

### Reproduction measure

A useful empirical measure is:

```text
Attributed new transacting sellers generated by a seller cohort
--------------------------------------------------------------
Eligible transacting sellers in that cohort
```

Also show distribution, not just an average, and time from a buyer’s payment to their first seller payment. A value above one in a short or biased sample does not prove sustainable viral growth. Retention, observation window, attribution, overlap and market saturation matter.

Do not call this a “network effect” just because one person referred another. Distribution and increasing product value from network size are separate claims.

### Quality filters

Exclude test mode, seed data, founder-funded demonstration transactions, known self-payments, reversed/duplicate charges and flagged circular payment patterns. Keep a visible reason for each exclusion. Do not silently delete suspicious records from financial accounting; exclude them only from the relevant growth measure.

### Admin presentation

1. Funnel with absolute counts and rates.
2. Matured cohort table.
3. First-payment cycle-time distribution.
4. Seller repeat-payment frequency and 30-day retention.
5. Gross amount, deduction and net contribution per active seller.
6. Privacy-preserving buyer-to-seller lineage table/graph with attribution confidence.

The graph should help answer “Did this actually propagate?” It must not become an excuse to build public discovery or a social feed.

## 31 — Durable jobs, recovery and observability

### Outbox design

Domain mutations and their required jobs are written in the same PostgreSQL transaction. Workers lease due jobs, process them outside long locks and mark completion safely. PostgreSQL supports row locking with `SKIP LOCKED`; reference D2. Our queue still needs explicit leases, retries, deduplication and recovery.

Jobs must be at-least-once safe. Do not claim exactly-once delivery across the database, a payment provider and an email service. Achieve one economic effect through stable references and database constraints.

### Required jobs

| Job | Behavior |
|---|---|
| Process provider inbox | Authenticate at ingress; process durable event with idempotent effects. |
| Verify unresolved payment | Bounded retries against the provider, then an operational exception. |
| Expire holds | Release only eligible unpaid holds, never confirmed reservations. |
| Expire offers | Transition actual eligible states with version checks. |
| Deliver transactional email | Dedupe, retry temporary errors and retain delivery result. |
| Check missing meeting links | Remind seller and escalate objective overdue delivery. |
| Send booking reminders | Re-read current schedule and delivery state before sending. |
| Reconcile collections | Compare expected records with provider evidence. |
| Reconcile settlements | Match allocations to provider settlement reporting. |
| Process expedited transfer | Exists only for an approved enabled route; never handles direct-split allocations. |
| Refresh public previews | Render sanitized versioned images; retry bounded failures. |
| Aggregate growth cohorts | Rebuildable from allowed events and verified financial facts. |
| Retention/privacy cleanup | Follow approved retention and legal-hold rules. |

Use exponential backoff with jitter and a maximum retry policy appropriate to the task. A money job that reaches its retry limit becomes visible to operations; it does not disappear. Stable idempotency keys survive worker crashes.

### Recovery scenarios

A worker may crash after an external service succeeds but before local completion is saved. Recover by checking the stable provider reference or delivery identity, not by creating a new transfer or payment automatically.

A webhook may arrive before the browser returns. A browser may never return. A webhook may be duplicated. Reconciliation may discover a payment the webhook missed. These are ordinary states, not rare hacks.

### Observability

Every request and job has a safe correlation ID. Structured logs include environment, service, operation, latency, outcome and relevant opaque record IDs. Exclude OTPs, raw tokens, bank account numbers, meeting URLs and private notes.

Monitor error rate, p95 API latency, payment verification age, inbox lag, job age, database pool pressure, email failure rate, settlement aging and reconciliation differences. Alert on actionable conditions, not every harmless retry.

Expose detailed metrics only internally. Public health endpoints must not reveal versions, configuration, database addresses or customer counts.

---

## 32 — Privacy, abuse and responsible access

### Keep the data small

Collect only what the flow needs. Do not require a biography, profession, identity document upload to our own storage or phone number without a real reason. Prefer provider-hosted identity collection where supported and approved.

Private notes can contain personal material even when the product did not request it. Restrict access and retention accordingly. Do not use notes to train models, recommend people or create marketing content.

### Retention

Define a data-class retention matrix before launch: account data, guest contact, notes, meeting links, security events, audit, financial records and raw provider payloads. Use approved legal/contractual retention periods rather than one delete-everything timer.

Delete or anonymize data when permitted and appropriate. A deletion request must not corrupt required financial history, unresolved cases or legitimate legal holds. Explain what is retained and why. Restrict raw provider payload retention and redact unnecessary fields.

### Abuse controls

Offer spam, impersonation, malicious meeting URLs, stolen payment credentials, circular self-payments and payout-destination takeover are relevant risks. Rate-limit by several signals without pretending a shared IP proves abuse. Provide a minimal block/report path for harassment or impersonation.

Do not allow the platform to become a disguised remittance product: payments must reference a real duration and booking. Do not support arbitrary transfers without time being booked. Never encourage bribery, paid influence over public decisions, access to confidential information, job-placement guarantees or other prohibited transactions.

“Anyone can sell time” does not mean anyone can provide regulated services without the necessary permissions. Keep appropriate acceptable-use boundaries and obtain jurisdiction-specific advice before advertising regulated categories. There is no need to create public expertise categories to enforce this.

### Operational restraint

Do not build routine surveillance of private calls. Do not record conversations by default. Do not ask for conversation transcripts to judge satisfaction. Gather only objective evidence necessary for a defined issue.

The application should be understandable as a scheduling/payment tool, but the actual allocation of legal responsibilities must be approved. Do not invent a regulatory exemption in the README, terms or UI.

---

## 33 — Performance, accessibility and search behavior

### Performance budgets: targets to verify, not achieved claims

Target public-page p75 LCP at or below 2.5 seconds, INP at or below 200ms and CLS at or below 0.1 on the measured user population. For local prelaunch checks, clearly identify lab conditions. Do not present a local Lighthouse score as proof of real-world field performance.

Practical engineering budgets:

- Keep the personal-link page’s critical JavaScript around or below 120KB compressed where feasible; explain measured exceptions.
- Avoid loading admin, charts, payment SDKs or calendar integrations on the homepage.
- Use responsive image derivatives and reserved dimensions.
- Load only necessary font files/weights; verify fallback metrics.
- Keep public pages server-rendered and cacheable where safe.
- Paginate app/admin lists on the server.
- Bound date-window queries and provider polling.

A slower network must not turn checkout into a blank spinner. Provide resumable states and useful messages. Do not cache authenticated payment/booking responses at a shared CDN.

### Accessibility

Aim for WCAG 2.2 AA and verify the implemented flows against reference A1. The target is not a certification claim.

Semantic headings, visible labels, useful error associations, keyboard-operable dialogs and calendars, visible focus, adequate contrast, reduced-motion support, zoom/reflow and appropriate announcements for async updates are required. Use native form controls where they work well. Placeholder text is not a label.

Dates, prices and statuses must not be communicated only through color or iconography. Ensure screen readers encounter the person, duration, time and total before the payment action. Font size and thin typography cannot sacrifice readability for aesthetics.

### Search and social previews

Marketing pages can be indexed and included in the sitemap. Personal links are publicly accessible to someone who has the URL, but default to **noindex** in V1 because discovery is not the product. Explain that “unlisted/noindex” is not private access control.

Private booking, offer, checkout, app and ops routes are authenticated/scoped, non-indexable and non-cacheable by shared caches. `robots.txt` is not an authorization mechanism.

Public social preview metadata must be present in the initial HTML. Canonical URLs, titles and descriptions come from sanitized public configuration. Do not include private relationship or payment information in metadata.

---

## 34 — Environments, configuration and deployment

### Three separate environments

Local, staging and production have separate databases, provider keys, storage buckets/prefixes, email settings, domains and webhook endpoints. Test-mode events cannot mutate live records. Production refuses test credentials and placeholder domains; staging displays a clear banner.

### Configuration contract

The final `.env.example` should document variables similar to these. They are configuration requirements, not supplied credentials:

```dotenv
APP_ENV=local
BRAND_NAME=Aside
PUBLIC_APP_URL=http://localhost:5173
API_INTERNAL_URL=http://api:8080
DATABASE_URL=
PORT=8080
TRUSTED_PROXY_CIDRS=

SESSION_SECRET=
OTP_PEPPER=
DATA_ENCRYPTION_KEY=
DATA_ENCRYPTION_KEY_VERSION=1

PAYMENT_PROVIDER=paystack
PAYSTACK_SECRET_KEY=
PAYSTACK_PUBLIC_KEY=
LIVE_PAYMENTS_ENABLED=false
PAYMENT_ROUTE=direct_split_settlement
EXPEDITED_PAYOUTS_ENABLED=false

FEE_POLICY_MODE=all_in_seller_deduction
FEE_BPS=500
FEE_POLICY_APPROVED=false
MIN_CHARGE_MINOR=
MAX_CHARGE_MINOR=
APPROVED_PAYMENT_CHANNELS=
SUBSIDY_BUDGET_MINOR=0

EMAIL_PROVIDER=local
EMAIL_FROM=
EMAIL_API_KEY=
SMTP_HOST=mailpit
SMTP_PORT=1025

STORAGE_ENDPOINT=
STORAGE_REGION=
STORAGE_BUCKET=
STORAGE_ACCESS_KEY=
STORAGE_SECRET_KEY=
PUBLIC_MEDIA_BASE_URL=

OPTIONAL_CALENDAR_ENABLED=false
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=

DEMO_MODE=true
ANALYTICS_ENABLED=true
ERROR_REPORTING_DSN=
OTEL_EXPORTER_OTLP_ENDPOINT=
```

Validate configuration at process startup. Reject `FEE_BPS` outside `0..500`, missing live commercial limits, inconsistent provider environments, enabled expedited payout without approved configuration and production demo mode.

Secrets remain server-side. Frontend public environment variables are not a secret store. Policy-approval state and commercial capability evidence should be auditable, not merely a developer’s unchecked boolean.

### Local developer experience

Provide working commands after implementation:

```bash
make bootstrap       # install pinned dependencies and check prerequisites
make dev             # start local web, API, worker and supporting services
make migrate         # apply local migrations intentionally
make seed-demo       # create clearly synthetic fixture data in non-production
make verify          # types, lint, build and focused invariant checks
make build           # reproducible application/container builds
```

Do not document commands that do not exist. `seed-demo` must refuse production. Local emails should be inspectable in the mail catcher. Local mock payments must be visibly marked as synthetic and impossible to enable silently in live mode.

### Deployment

Use a reverse proxy with HTTPS and explicit routing. Run API and worker separately so jobs do not depend on a web request’s lifetime. Configure timeouts, request limits, graceful shutdown, connection pools and retry policy.

Database migrations are deliberate deployment steps, not a race run by every container replica. Back up before destructive changes and prefer expand/contract migrations. Use non-root containers, private database networking and least-privilege credentials.

Keep the new product isolated from unrelated production applications. Do not install it into Kredit’s production database just because access is available.

### Caching

Cache only sanitized public data. A cached personal page cannot include a seller-only edit control, private email or authenticated navigation. Render private account context separately with proper cache controls.

Pricing/image version changes invalidate public caches. Checkout always revalidates against the backend. Availability caches are short-lived and never replace the reservation constraint. Sensitive responses use `Cache-Control: no-store` as appropriate.

### Recovery and operational readiness

Automated encrypted backups, an actual restore exercise, secret rotation procedure, provider-key rotation procedure, monitoring, incident contacts and a global pause-new-checkouts control are required before live money.

Pausing new collections must not stop processing existing webhooks, reversals or settlement reconciliation. A payment-provider outage should disable new checkout safely while preserving existing records and the public person page where sensible.

---

## 35 — Step-by-step build order

Do not build by randomly selecting pages. Deliver complete slices in this order. At each stage, record files changed, real verification evidence, unresolved issues and external blockers. Do not claim later stages are complete because the UI has placeholder data.

### Stage 0 — Inspect, decide and establish the truth

**Create:** `docs/DECISIONS.md`, `docs/PROVIDER_CAPABILITIES.md`, initial implementation checklist and a route/scope inventory.

Inspect any existing repository and environment. Confirm the working-name placeholder, selected stack, fee interpretation, initial payout route and baseline meeting method. Read the primary provider/framework documentation in Section 40. Record which capabilities are documented, tested, commercially approved or unknown.

Do not wait for a final brand/domain to build local software. Use configuration. Do not enable live collections while the fee policy, merchant approval, legal entity or settlement arrangement is unresolved.

**Done when:** there is a coherent local build plan, no incompatible architectural assumptions, and a clear list of external launch gates.

### Stage 1 — Foundation and design language

**Create:** monorepo, pinned dependencies, local services, web/API/worker startup, basic CI/build commands, brand configuration, CSS tokens, typography, icons and `/dev/ui` gallery.

Implement button, input, money field, duration picker, status, date/time components, panel, dialog, table and notices. Verify responsive behavior and accessibility of these primitives before repeating them everywhere.

Build a polished static homepage composition and public-person shell using clearly labeled demo data. This is the visual foundation, not a claim that the product is functional yet.

**Done when:** the three processes start cleanly, the design system is coherent at mobile/desktop sizes, and the initial pages look intentionally designed rather than default components.

### Stage 2 — Data, identity and account boundaries

**Create:** initial migrations, sqlc queries, sessions, email challenges, guest scopes, role/permission checks and admin bootstrap.

Implement email verification, account login/logout, scoped guest identity, handle claiming, reserved handles, seller profile draft and explicit buyer-to-seller capability creation. Set up the mail catcher locally and actual email adapter boundaries.

**Done when:** a person can sign in and claim a link; two people cannot claim the same handle; a guest cannot see someone else’s booking; a normal user cannot enter ops.

### Stage 3 — Seller setup and the real personal page

**Create:** link editor, pricing versions, duration calculations, publication/readiness state, availability rules and payout-onboarding UI states.

Build the real personal route using database data. Implement the fixed-price and offer-mode public variants. Add avatar processing, optional identity link, pause/unpause and private preview. The final meeting/payout integrations can remain explicitly blocked, but their states must be real.

**Done when:** profile edits persist, public caches update, fee/price calculations are consistent, an unready seller cannot collect money, and no expertise fields have appeared.

### Stage 4 — Scheduling and guest booking without live money

**Create:** server slot generation, database reservations, quote lifecycle, guest checkout details, booking page shells and state machines.

Implement weekly hours, date overrides, timezone handling, minimum notice, horizon, buffers, expiring holds and atomic overlap protection. Wire all screens to the backend. Use a labeled local payment simulator solely for development.

**Done when:** two concurrent buyers cannot reserve the same occupied interval; timezones and buffers behave correctly; stale/expired slots have real recovery flows; guests do not need a seller account.

### Stage 5 — Payment collection and direct settlement integration

**Create:** approved provider adapter, server checkout initialization, raw-body webhook verification, durable inbox/outbox, verification worker, economic allocation and ledger.

Implement the fee ceiling, destination snapshots, one-route invariant, duplicate/late-payment exceptions and provider settlement reporting. Use sandbox credentials supplied by the owner. Do not assume test support for every settlement feature; document capabilities that need approved live verification.

**Done when:** a genuine provider-sandbox transaction traverses checkout → verification → booking → allocation; repeated webhooks do not duplicate money; direct split does not trigger a transfer; the UI does not label allocation as bank-paid.

### Stage 6 — Offer mode end to end

**Create:** verified guest offer submission, received/sent offers, one counteroffer, agreement expiry and agreed-price checkout.

Implement the exact state machine and stale-version protection. Accepting an offer does not charge the buyer. Accepted terms survive later public price changes. Each offer converts at most once.

**Done when:** a visitor can offer, the seller can accept/counter/decline, and an agreed offer produces one correctly priced paid booking through the same payment foundation.

### Stage 7 — Logged-in app and conversation delivery

**Create:** overview, giving/booking tabs, offer inbox, money view, profile editor, availability UI, settings and booking detail.

Implement booking-specific meeting link entry, deadlines, notifications, ICS, rescheduling, cancellation/issue entry and participant completion signals. Build buyer-only states without forcing a seller profile. Show real settlement data and honest unknown states.

**Done when:** seller and buyer can conduct and manage a full conversation lifecycle, all relevant pages work on mobile, and no essential action is a toast-only mock.

### Stage 8 — Minimum real operations

**Create:** ops login with MFA, overview queues, people, bookings, payments, settlement reconciliation, objective exceptions, audit and controlled settings.

Wire role checks in Go. Add safe provider recheck, account restriction, session revocation, evidence export and provider-deadline alerts. No generic balance edit, arbitrary payout or mark-paid control.

**Done when:** an operator can understand and resolve a defined payment/delivery issue without SQL edits; every sensitive action is audited; permissions hold at the API level.

### Stage 9 — Sharing and measured buyer-to-seller growth

**Create:** public OpenGraph images, share cards, copy/native share actions, confirmation CTA, first-payment prompt and growth event pipeline.

Implement verified identity linking, attribution coverage, self/test-payment exclusions and matured buyer-to-seller cohorts. Build the internal lineage view only from supported relationships.

**Done when:** a real buyer can deliberately create their own link and the system can record their first genuine seller payment without pretending copy clicks or signups are virality.

### Stage 10 — Visual refinement across the whole product

Use actual populated and empty states. Review every public, guest, app and ops route at the required widths. Fix typography, spacing, overflow, hierarchy, keyboard use, transitions, error states and content.

Compare the five acceptance scenes in Section 04. The public page should not resemble the admin. The admin should not inherit decorative hero typography. Payment details should remain legible. Remove every accidental cliché and every invented statistic.

**Done when:** screenshots show a coherent designed product across the full flow, not one polished homepage followed by default forms.

### Stage 11 — Production-readiness verification

Complete the checks in Section 36, document actual outputs, verify deployment/backup recovery, and review provider and commercial gates. Conduct a constrained end-to-end provider test appropriate to the approved environment.

Do not chase a vanity test-coverage percentage. Use focused automated checks for money, permissions, concurrency and state transitions, plus hands-on visual/usability verification. Record limitations honestly.

**Done when:** critical invariants are demonstrated, unresolved risks are visible, no secret/test data leak exists, and the owner has a precise live-launch checklist.

### Stage 12 — Owner-approved private beta

Only after authorization, deploy to the approved domain and merchant environment. Enable a small group of real sellers who already receive requests for time. Do not manufacture payments or bulk-message strangers.

Monitor complete flows, unresolved exceptions, seller delivery and the real buyer-to-seller funnel. Support the first real users with the minimum operational tools already built.

**Done when:** the product is genuinely usable, not merely deployed, and evidence can distinguish adoption from curiosity.

---

## 36 — Verification matrix

These are required checks, not claims that verification has already happened. Record the environment and evidence for each. Prefer small targeted checks over a sprawling testing project.

### Product and access

| Check | Expected result |
|---|---|
| Claim a valid handle | One owner, persisted configuration, real public route. |
| Concurrent claim of one handle | One success; one clear conflict. |
| Claim reserved/system handle | Rejected server-side. |
| Open fixed-price link as guest | Person, duration, price and booking action work without seller signup. |
| Open offer link as guest | Amount/duration flow; verified sender; no charge before agreement. |
| Buyer becomes seller | Explicit action; verified identity; own profile, not a duplicate account. |
| Pause link | New transactions stop; existing private bookings remain accessible. |
| View another person’s private record | Denied even if opaque ID is known. |
| Enter another person’s email | Does not expose that person’s history. |
| Normal user requests ops API | Denied; frontend hiding is not the security control. |

### Money and state

| Check | Expected result |
|---|---|
| Change amount in browser request | Server quote amount wins or request is rejected. |
| Set fee above 500 basis points | Rejected at config/domain/database boundary. |
| Fee rounding on small/odd minor-unit amount | Deduction never exceeds cap; allocation balances. |
| Processor cost exceeds allowed deduction | Blocked or explicitly approved subsidy; no hidden seller deduction. |
| Duplicate webhook | One economic allocation and one booking confirmation. |
| Different event payload for same successful transaction | No duplicate money effect. |
| Webhook arrives before return redirect | Booking still confirmed correctly. |
| Browser never returns | Durable webhook/reconciliation still processes payment. |
| Webhook storage fails | Retryable failure; event not silently lost. |
| Payment status unresolved | No false failure, no accidental second charge. |
| Late successful payment, slot occupied | Needs-attention exception; no double booking. |
| Wrong currency or amount | Quarantined; no normal entitlement allocation. |
| Two successful charges for intended booking | One normal allocation; duplicate charge exception. |
| Direct split plus transfer request | Transfer refused by route invariant. |
| Transfer timeout | Requery/stable-reference recovery; no new speculative payout. |
| Transfer reversed | Correct compensating record and obligation state. |
| Bank destination changed during pending settlement | Approved immutable-routing procedure; no silent rerouting. |
| Price changes after an accepted offer | Accepted immutable terms are preserved. |
| Old counteroffer accepted after terminal action | Rejected as stale. |
| Admin attempts ledger edit/delete | Not permitted. |

### Scheduling and delivery

| Check | Expected result |
|---|---|
| Two concurrent requests for one occupied interval | Database permits one active reservation only. |
| Adjacent slots with buffer | No forbidden overlap. |
| Expiry worker sees confirmed booking | Booking remains reserved. |
| Seller edits hours | Existing bookings preserved. |
| DST spring-forward date | Nonexistent local slots not offered. |
| DST fall-back date | Repeated local times correctly disambiguated. |
| Reschedule proposal conflicts before acceptance | Original remains valid; alternative requested. |
| Meeting-link provider/addition fails | Honest pending/overdue state and alert. |
| Reminder after reschedule | Uses current date/time/link; old job does not send stale details. |
| Scheduled end passes | Window elapsed, not invented proof of attendance. |

### Visual and accessibility

Review homepage, fixed link, offer link, checkout, confirmation, guest booking, onboarding, overview, offers, money and payment admin detail at mobile and desktop widths. Include long names, a missing photo, large amounts, no availability, 100+ bookings, provider errors and a buyer-only account.

Keyboard-complete booking, visible focus, readable validation, screen-reader labels, reduced motion, 200% zoom, 320px layout, mobile keyboard behavior, loading/resume and no horizontal overflow outside deliberate table regions.

### Operations and privacy

Verify backup restoration, environment isolation, redacted logs, scoped export, upload rejection, guest token expiry, session revocation, rate limits, admin MFA, email delivery failure, provider outage, reconciliation differences and analytics exclusion of synthetic/circular transactions.

---

## 37 — Demonstration data and visual review fixtures

Use synthetic data only. Fixtures must be marked with `is_demo`/environment identifiers and excluded from production/growth measures. The production startup/seed tools must prevent accidental seeding.

### People

- **Tomi:** fixed price, ₦10,000 for 30 minutes, several real future sample slots.
- **Ada:** offer mode, one pending and one countered sample offer.
- **Seyi:** new account with a claimed link and incomplete payout readiness.
- **Nneka:** buyer-only account with an upcoming booking.
- **Femi:** paused page.
- **Long-name fixture:** proves wrapping and share-image overflow handling.

These are fictional examples, not customer testimonials. Use initials unless supplied approved images exist. Do not download random social profile photographs.

### Transaction fixtures

Create labeled examples of unpaid, pending, confirmed, late-paid-needs-attention, allocated-awaiting-settlement, provider-confirmed-settled, failed transfer, reversed transfer, duplicate-charge exception and a simple synthetic buyer-to-seller lineage.

The growth graph in demo mode must say **Demo data**. A screenshot of the graph must not be reused as evidence of traction.

### Screenshot artifacts

Save actual rendered captures under `docs/screenshots/`, with environment, viewport and date. Suggested filenames:

```text
home-desktop.png
home-mobile.png
person-fixed-mobile.png
person-offer-mobile.png
checkout-review-mobile.png
booking-confirmed-mobile.png
seller-overview-desktop.png
seller-overview-mobile.png
offer-detail-mobile.png
money-desktop.png
ops-payment-detail-desktop.png
ops-growth-demo-desktop.png
```

Capture actual pages with available browser tooling. Do not fabricate screenshots or claim visual inspection when no browser was available. If browser tooling is absent, record the limitation and leave the visual gate open.

---

## 38 — Launch experiment and decision dashboard

### First cohort

Recruit a small group of people who already receive genuine unsolicited requests for their time. Do not define eligibility by a follower-count threshold or a professional title. The useful criterion is existing inbound demand.

Give them their real link and ask them to use it when an appropriate request arrives. Do not require friends to transact to make the graph look healthy. Any owner-funded demonstration payment is labeled and excluded.

### Observe these steps

```text
Real request for time
  -> seller chooses to share link
  -> visitor opens link
  -> amount/time or offer selected
  -> verified payment
  -> booking delivered
  -> seller uses link again
  -> eligible buyer becomes ready seller
  -> new seller receives first genuine payment
```

The first two steps are not fully observable in software. Collect a light seller-reported measure or interview where needed. Do not fill missing denominators with guesses.

### The useful decision view

Show actual paid bookings, active sellers, repeat-paying buyers, seller repeat use, delivery issues, settlement delays, contribution after processing, buyer-to-seller activation and cycle time. Separate people who create a link from people who receive money.

One hundred genuine transactions is a useful initial learning target, not statistical proof of a large market. A handful of multi-generation chains is a reason to investigate, not to declare the company viral.

### Experiment discipline

Start with one clear default onboarding and one confirmation CTA. Add a second copy variant only after instrumentation works. Assign variants consistently, keep a small holdout where practical, and measure downstream first-payment activation—not merely button clicks.

Do not run so many simultaneous variants that a small beta cannot explain its own results. Do not change pricing, settlement policy, onboarding and growth copy all at once and then claim to know what caused a change.

---

## 39 — Final decision register and agent handoff

### Decisions requiring owner approval before live launch

| Decision | Proposed implementation/default | What remains to approve |
|---|---|---|
| Final name/domain | Configurable working name Aside; reserved example domain. | Name, domain ownership and branding clearance. |
| Fee interpretation | Total ordinary seller deduction capped at 5%, processor costs absorbed. | Commercial feasibility and exact public wording. |
| Merchant/legal identity | Not invented or borrowed from another project. | Correct contracting entity, provider account and required policies. |
| Payment-provider model | Paystack direct split as initial adapter. | Approval for this precise model and seller onboarding. |
| Payout speed | Honest actual provider settlement. | Any expedited route, funding, risk and provider agreement. |
| Transaction limits/channels | Configurable; no unapproved live defaults. | Minimum, maximum, channels, subsidy budget and fraud policy. |
| Meeting delivery | Private per-booking seller-supplied link. | Whether beta reliability warrants automatic calendar provisioning first. |
| Required remedies/retention | Minimal objective exceptions; no subjective marketplace. | Legal/provider responsibilities and retention periods. |
| Public launch | Private staging first. | Explicit deployment and collection authorization. |

Unknowns above are **live-launch blockers, not excuses to stop local implementation**. Build the safe state and surface the limitation.

### Definition of done

The product is done for beta only when a ready seller can share a personal link, a guest can pay for an actually available time or submit an offer, both parties receive correct instructions, the seller can see honest money status, operations can resolve objective failures, and a buyer can deliberately become a seller—with all of it backed by real persistence and permissions.

A beautiful landing page alone is not done. A complete backend with generic or broken screens is not done. A fake dashboard with invented money is not done. A successful checkout without reconciliation is not done. A viral-looking signup graph without genuine payments is not done.

### What the agent must deliver

Working source code, migrations, configuration example, repeatable local setup, OpenAPI contract, clear implementation status, provider capability notes, money-flow documentation, secure operations instructions, deployment/rollback guidance, focused verification evidence and actual screenshots when tooling permits.

End the implementation handoff with:

```text
Implemented:
  [Specific working flows and routes]

Verified:
  [Actual checks, environments and results]

External blockers:
  [Credentials, approval, domain or provider capability still missing]

Not implemented:
  [Explicit scope exclusions and unfinished work]

How to run:
  [Commands that were actually created and exercised]

How to review:
  [Local URLs, safe demo access procedure and screenshot locations]

Financial safety status:
  [Fee cap, idempotency, route exclusivity and reconciliation evidence]
```

Do not include real secrets or production credentials in that handoff. Do not hide unfinished work behind “production-ready” language.

### Suggested instruction to start the coding agent

> Read this entire README before changing code. Implement the product in the stage order in Section 35, preserving the simple paid-time-link model and the visual direction. Start by inspecting the repository and recording decisions, then build the foundation and first complete flow. Keep a persistent implementation checklist and continue through unblocked stages. Use real persistence and provider sandbox integrations where credentials are supplied. Do not add expertise fields, a marketplace, courses, a wallet, AI features or a mobile app. Do not fake payments, payouts, growth, approvals or completed work. Record external blockers and keep live-money features disabled until approved.

---

## 40 — Primary reference register

The pages below were consulted when preparing this specification on **22 September 2026**. They document provider/framework behavior, not this product’s commercial approval or actual implementation. Recheck live documentation and the specific merchant agreement before integrating. Design choices, product defaults, schemas and growth measures in this README are proposed implementation requirements, not claims made by these sources.

### Payments

| ID | Primary source | Used for |
|---|---|---|
| P1 | Paystack Nigeria pricing and settlement FAQ | Published fee schedule and standard settlement distinction. |
| P2 | Paystack split payments guide | Allocation, fee-bearing configuration and subaccounts. |
| P3 | Paystack verify payments guide | Server verification rather than trusting browser return. |
| P4 | Paystack transaction API | Current initialization/verification request fields. |
| P5 | Paystack webhook guide | Raw-payload signature mechanism and event behavior. |
| P6 | Paystack how transfers work | Available balance, transfer states and stable-reference recovery. |
| P7 | Paystack subaccount API | Current subaccount fields; not automatic legal approval. |
| P8 | Paystack settlement API | Provider settlement and subaccount reporting. |
| P9 | Paystack manage disputes | Unavoidable provider exception process. |
| P10 | Paystack refunds guide | Objective remedy/reversal implementation when required. |

```text
P1  https://paystack.com/pricing
P2  https://paystack.com/docs/payments/split-payments/
P3  https://paystack.com/docs/payments/verify-payments/
P4  https://paystack.com/docs/api/transaction/
P5  https://paystack.com/docs/payments/webhooks/
P6  https://paystack.com/docs/transfers/how-transfers-work/
P7  https://paystack.com/docs/api/subaccount/
P8  https://paystack.com/docs/api/settlement/
P9  https://paystack.com/docs/payments/manage-disputes/
P10 https://paystack.com/docs/payments/refunds/
```

### Frontend, database, security and calendar

| ID | Primary source | Used for |
|---|---|---|
| F1 | SvelteKit introduction | Current framework structure and capabilities. |
| F2 | Tailwind SvelteKit installation guide | Compatible styling setup. |
| F3 | shadcn-svelte SvelteKit installation | Current accessible-component setup. |
| F4 | SvelteKit form actions | Progressive form behavior and server action implementation. |
| F5 | SvelteKit auth guidance | Server hooks and session integration. |
| D1 | PostgreSQL range types | Range/exclusion-constraint scheduling approach. |
| D2 | PostgreSQL SELECT documentation | Worker row locking and SKIP LOCKED semantics. |
| S1 | OWASP Session Management Cheat Sheet | Session security baseline. |
| A1 | W3C WCAG 2.2 quick reference | Accessibility target and verification criteria. |
| C1 | Google Calendar create-events guide | Optional event/conference provisioning. |

```text
F1  https://svelte.dev/docs/kit/introduction
F2  https://tailwindcss.com/docs/installation/framework-guides/sveltekit
F3  https://www.shadcn-svelte.com/docs/installation/sveltekit
F4  https://svelte.dev/docs/kit/form-actions
F5  https://svelte.dev/docs/kit/auth
D1  https://www.postgresql.org/docs/current/rangetypes.html
D2  https://www.postgresql.org/docs/current/sql-select.html
S1  https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html
A1  https://www.w3.org/WAI/WCAG22/quickref/
C1  https://developers.google.com/workspace/calendar/api/guides/create-events
```

Earlier conversation-level competitor claims, user counts and market-size estimates are intentionally not used as build requirements or marketing evidence. They would require independent verification before publication.

---

## The last instruction

Make it feel like someone cared about every line, every gap and every state.

But keep the product small enough to explain in one message:

> **They want your time. You send your link.**

Everything that does not support that should earn its place—or stay out.
