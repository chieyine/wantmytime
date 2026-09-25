<script lang="ts">
	import PageMeta from '$lib/components/PageMeta.svelte';
</script>

<PageMeta title="Privacy — WantMyTime" description="What WantMyTime collects, why, how long we keep it, and how to export or delete it." />
<section class="article-page">
	<p class="eyebrow">Privacy</p>
	<h1 class="page-heading">Privacy, in plain terms.</h1>
	<p>This notice follows the Nigeria Data Protection Act 2023.</p>

	<h2>Who we are</h2>
	<p>WantMyTime is run by Kredit Technologies Limited (RC 9834452), House No. 348, Jamaina Road, Pompomari Bypass, Maiduguri, Borno State, Nigeria. We decide how your personal data is used, which makes us the data controller. Our data protection officer can be reached at <a class="text-link" href="mailto:privacy@wantmytime.com">privacy@wantmytime.com</a>.</p>

	<h2>What we collect</h2>
	<ul>
		<li><b>Your account:</b> your email address and name, and the sign-in sessions you open.</li>
		<li><b>If you take bookings:</b> your link, photo, profile link, prices, available hours, cancellation policy and bank account for payouts. We store the full account number encrypted and show only its last four digits.</li>
		<li><b>Bookings and offers:</b> who booked whom, when, for how long and for how much; meeting links; reported problems, no-shows, cancellations and reviews.</li>
		<li><b>Payments:</b> payment references, amounts and status from our payment provider. We do not take card payments, and we never see your bank login or mobile money PIN.</li>
		<li><b>Google Calendar, only if you connect it:</b> when your calendar is busy (not what your events are), plus an encrypted access grant.</li>
		<li><b>Notifications, only if you switch them on:</b> the address your browser gives us for delivering notifications to that device, and which notifications were sent. Turning them off, or deleting your account, removes it.</li>
		<li><b>Product and security records:</b> anonymous product events (for example “link copied”), the actions taken on your account, and request logs used to keep the service secure.</li>
	</ul>

	<h2>Why we use it, and on what legal basis</h2>
	<ul>
		<li><b>To run bookings and payments you ask for</b> (performing our contract with you): sign-in, booking pages, emails about your sessions, collecting payment and paying hosts.</li>
		<li><b>To meet legal duties</b>: keeping payment, refund and payout records for tax and anti-money-laundering law, and answering lawful requests.</li>
		<li><b>For our legitimate interests</b>: preventing fraud and abuse, keeping the service secure, and understanding which features are used, using only anonymous events.</li>
		<li><b>With your consent</b>: connecting Google Calendar. You can disconnect at any time.</li>
	</ul>
	<p>We do not sell personal data, show advertising, or use your data to train AI models.</p>

	<h2>Who we share it with</h2>
	<p>Only the service providers we need, each under a contract that limits what they may do with it:</p>
	<ul>
		<li><b>Kora</b> (payments and payouts; Nigeria, and the other African countries where sellers are paid).</li>
		<li><b>Cloudflare</b> (hosting of profile photos and protection of the site).</li>
		<li><b>Our email provider</b> (sign-in codes and booking emails).</li>
		<li><b>Google</b>, only if you connect Google Calendar.</li>
		<li><b>Your browser’s notification service</b> (Google, Apple, Mozilla or Microsoft, depending on your device), only if you switch notifications on. Each notification is encrypted so that only your device can read it.</li>
		<li><b>Our error-reporting service</b>, which receives technical error details without form contents.</li>
		<li><b>Our hosting and database provider.</b></li>
	</ul>
	<p>Some of these providers process data outside Nigeria. When they do, we rely on the safeguards the Act allows, such as contractual protections, and we choose providers with strong security.</p>
	<p>The person you book sees the name and email you give when booking. People who visit a host’s page see only what the host chose to publish.</p>

	<h2>Google Calendar</h2>
	<p>If you connect Google Calendar, WantMyTime reads only when your primary calendar is busy, not what your events are, so booked times don’t clash with your other plans. It adds an event for each booking, with a Google Meet link, to a calendar you own, and updates or removes that event when the booking changes. Buyers aren’t added as guests. WantMyTime stores an encrypted access grant and the busy time ranges for your booking window, and deletes both when you disconnect. Disconnecting also revokes WantMyTime’s access at Google. Events already added stay on your calendar.</p>
	<p>WantMyTime’s use and transfer of information received from Google APIs adheres to the <a href="https://developers.google.com/terms/api-services-user-data-policy">Google API Services User Data Policy</a>, including the Limited Use requirements. This data is used only to provide these calendar features. It is never sold, used for advertising, or read by people except to fix a problem you ask us to fix, for security, or where the law requires it.</p>

	<h2>How long we keep it</h2>
	<ul>
		<li>Sign-in codes: deleted a day after they expire.</li>
		<li>Sign-in sessions: deleted 30 days after they end.</li>
		<li>Meeting links: removed 30 days after the session.</li>
		<li>Names on unpaid holds: removed after 90 days. Emails on closed offers: removed after 180 days.</li>
		<li>Anonymous product events: 13 months.</li>
		<li>Payment, refund, payout and accounting records: six years, as tax and anti-money-laundering law requires. If you delete your account, your name and email are removed from them.</li>
		<li>Backups: deleted data can remain in encrypted backups for up to 14 days before they are replaced.</li>
	</ul>

	<h2>Your rights</h2>
	<p>You can ask to see, correct or delete your data, to receive it in a portable file, to restrict or object to how we use it, and to withdraw consent you gave. Most of this you can do yourself:</p>
	<ul>
		<li><b>Download your data</b> or <b>delete your account</b> from <a class="text-link" href="/app/settings/data">Settings → Your data</a>.</li>
		<li>Correct your name, link details and hours from your workspace.</li>
		<li>For anything else, email <a class="text-link" href="mailto:privacy@wantmytime.com">privacy@wantmytime.com</a>. We reply within 30 days.</li>
	</ul>
	<p>If you are unhappy with how we handled your data, you can complain to the <a href="https://ndpc.gov.ng">Nigeria Data Protection Commission</a>. If you live in another country, you can also complain to the data protection authority there, and you have the rights its law gives you as well as these.</p>

	<h2>Security</h2>
	<p>Connections are encrypted. Bank account numbers, calendar grants, meeting links and operator security keys are encrypted in our database with separate keys. Sign-in uses one-time email codes, and operator access needs a second factor. If a breach puts your data at risk, we will tell the Commission within 72 hours and tell you without delay.</p>

	<h2>Children</h2>
	<p>WantMyTime is for people aged 18 and over. We do not knowingly collect data from children.</p>

	<h2>Changes</h2>
	<p>If we change this notice in a way that matters, we will email account holders before the change takes effect. Last updated: 25 September 2026.</p>
</section>
