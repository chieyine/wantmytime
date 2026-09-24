# WantMyTime design system

The source of truth is `apps/web/src/styles.css`. The visual direction treats time as a personal instrument: a working clock, editorial typography, fine rules, and a restrained palette. It avoids decorative cards and fake social proof.

- Canvas `#f2f0e9`, ink `#171817`, secondary text `#444744`, signal red `#b43a30`. Red marks live time and selection; it is not a page background.
- Helvetica Neue/system sans carries interface text. Baskerville/serif is reserved for expressive names and the home headline. Monospace labels identify time, steps, and metadata.
- The home page has one clear action: claim a link. The clock is live; choosing 15, 30, or 60 minutes redraws the marked arc and explains the choice. Public profiles use the same duration behavior.
- The public profile uses identity on one side and booking decisions on the other. The booking flow keeps date, time, personal details, and total visible in one hierarchy.
- Signed-in seller pages share one workspace frame: an ink navigation rail on desktop, a compact section bar on smaller screens, and consistent page widths, forms, lists, notices and empty states. Account actions stay visible in the shared frame.
- The overview reports live profile checks, weekly hours, provider configuration, and recent booking and offer records. It labels recent-record limitations rather than implying a global total.
- Availability is a seven-day editor with time bars, a selected-day editor, booking limits, date exceptions and a visible saved/unsaved state. The link editor previews draft changes instantly and distinguishes them from published changes.
- Bookings and offers present status, deadline, participant and next action before secondary details. Seller booking details remain in the workspace; the guest booking route uses the same underlying component.
- Money treats an allocation, an item awaiting settlement evidence, and a provider-confirmed settlement as distinct states. Confirmation requires the matched `settled_at` evidence returned by the service.
- The same neutral tokens carry through seller and operations screens. Focus indicators remain blue to distinguish keyboard focus from brand accents.
- Motion describes time and progress: the clock assembles on arrival, its hands keep local time, the duration arc responds to a choice, and the method rail advances with scroll position. These animations should not block reading or controls.
- Motion respects `prefers-reduced-motion`; the clock still displays time without smooth sweeping when reduced motion is requested.
- Controls retain at least 48px targets where practical, and the primary layout adapts below 1000px and 720px.

The app currently uses system font fallbacks. If licensed brand font files are added later, self-host them and check their rendering across operating systems before changing these stacks.
