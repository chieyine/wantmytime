<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { timezoneOptions } from '$lib/timezones';
	import { currencySymbol } from '$lib/money';
	import PublicPersonPage from '$lib/components/PublicPersonPage.svelte';
	let handle=$state(''); let name=$state(''); let identity_url=$state(''); let mode=$state<'fixed'|'offer'>('fixed'); let amount=$state(10000); let durations=$state<number[]>([15,30,60]); let timezone=$state('UTC'); let currency=$state('NGN'); let paused=$state(false); let exists=$state(false); let loading=$state(true); let saving=$state(false); let message=$state('');
	let ready=$state(false); let activeView=$state<'edit'|'preview'>('edit');
	let avatarPreview=$state(''); let avatarMessage=$state(''); let avatarBusy=$state(false); let avatarInput=$state<HTMLInputElement>();
	let savedFingerprint=$state(''); let saveState=$state<'idle'|'saved'|'error'>('idle');
	let fingerprint=$derived(JSON.stringify({name,identity_url,mode,amount:mode==='fixed'?Math.round(Number(amount||0)*100):0,durations,timezone,paused}));
	let dirty=$derived(exists && !loading && fingerprint!==savedFingerprint);
	let identityLabel=$derived.by(()=>{try{const h=new URL(identity_url).hostname.toLowerCase().replace(/^www\./,'');return ({'instagram.com':'Instagram','linkedin.com':'LinkedIn','x.com':'X','twitter.com':'X','tiktok.com':'TikTok','youtube.com':'YouTube','github.com':'GitHub'} as Record<string,string>)[h]??''}catch{return ''}});
	let previewPerson=$derived({handle,name,identity_url,identity_label:identityLabel,avatar_url:avatarPreview,mode,base_30_minor:mode==='fixed'?Math.round(Number(amount||0)*100):0,durations,timezone,paused,ready,currency,local_simulator:false});
	function toggle(d:number){durations=durations.includes(d)?durations.filter(x=>x!==d):[...durations,d].sort((a,b)=>a-b)}
	onMount(async()=>{
		try {
			const p=await api<{handle:string;name:string;identity_url:string;mode:'fixed'|'offer';base_30_minor:number;durations:number[];timezone:string;paused:boolean;ready:boolean;avatar_version?:number;currency?:string}>('/api/v1/me/link');currency=p.currency||'NGN';
			handle=p.handle;name=p.name;identity_url=p.identity_url||'';mode=p.mode;amount=p.base_30_minor/100;durations=p.durations;timezone=p.timezone;paused=p.paused;ready=p.ready;exists=true;
			savedFingerprint=JSON.stringify({name:p.name,identity_url:p.identity_url||'',mode:p.mode,amount:p.mode==='fixed'?p.base_30_minor:0,durations:p.durations,timezone:p.timezone,paused:p.paused});
			const version=p.avatar_version||0;if(version)avatarPreview=`/api/v1/people/${encodeURIComponent(handle)}/avatar?v=${version}`;
		} catch (error) {
			const text=error instanceof Error?error.message:'';
			if (!text.includes('not claimed')) message=text || 'Your link could not be loaded.';
		} finally { loading=false; }
	});
	async function save(){
		if(!dirty)return;
		saving=true;message='';
		const submittedFingerprint=fingerprint;
		const payload={handle,name,identity_url,mode,base_30_minor:mode==='fixed'?Math.round(Number(amount||0)*100):0,durations:[...durations],timezone,paused};
		try {
			await api('/api/v1/me/link',{method:'PATCH',body:JSON.stringify(payload)});
			savedFingerprint=submittedFingerprint;saveState='saved';
			message='Your changes are saved.';
		} catch(error) { saveState='error';message=error instanceof Error?error.message:'Your changes could not be saved.'; }
		finally { saving=false; }
	}
	async function uploadAvatar(){const file=avatarInput?.files?.[0];if(!file)return;avatarMessage='';if(!['image/png','image/jpeg'].includes(file.type)){avatarMessage='Choose a PNG or JPEG image.';return}if(file.size>2*1024*1024){avatarMessage='Choose an image smaller than 2 MB.';return}avatarBusy=true;try{const data=new FormData();data.append('avatar',file);const response=await fetch('/api/v1/me/avatar',{method:'PUT',body:data});const body=await response.json();if(!response.ok)throw new Error(body?.error?.message||'The image could not be saved.');avatarPreview=`${body.url}?v=${body.avatar_version}`;avatarMessage='Profile image saved.'}catch(e){avatarMessage=e instanceof Error?e.message:'The image could not be saved.'}finally{avatarBusy=false}}
async function removeAvatar(){avatarBusy=true;avatarMessage='';try{const response=await fetch('/api/v1/me/avatar',{method:'DELETE'});if(!response.ok)throw new Error('The image could not be removed.');avatarPreview='';avatarMessage='Profile image removed.'}catch(e){avatarMessage=e instanceof Error?e.message:'The image could not be removed.'}finally{avatarBusy=false}}
</script>
<svelte:head><title>Your link — WantMyTime</title></svelte:head>
<section class="form-page app-page"><a class="back-link" href="/app">← Overview</a><p class="eyebrow">Your link</p><h1 class="page-heading">A page that sounds like you.</h1>
	{#if loading}<p class="page-intro">Loading your link…</p>
		{:else if !exists}<p class="page-intro">You have not claimed a link yet. Set up your name, link, price and conversation lengths.</p><a class="button" href="/claim">Create your link ↗</a>{#if message}<div class="notice notice-warning" role="alert">{message}</div>{/if}
	{:else}<div class="link-editor-status"><div><span class:dirty class:saved={saveState==='saved'&&!dirty} class="link-editor-status-dot"></span><strong>{saving?'SAVING':dirty?'UNSAVED CHANGES':saveState==='saved'?'CHANGES SAVED':'ALL CHANGES SAVED'}</strong></div><p>Changes appear in the preview immediately. Your public page updates when you save.</p></div><div class="link-editor-tabs" aria-label="Link editor view"><button class:active={activeView==='edit'} onclick={()=>activeView='edit'}>Edit your page</button><button class:active={activeView==='preview'} onclick={()=>activeView='preview'}>Preview</button></div><div class="link-editor-layout"><form class="setup-form link-editor-form" class:mobile-hidden={activeView==='preview'} onsubmit={(e)=>{e.preventDefault();save()}}>
		<label>Handle<input class="field" value={handle} readonly /></label><label>Display name<input class="field" bind:value={name} maxlength="80" required /></label><label>Profile photo · optional<input class="field" bind:this={avatarInput} type="file" accept="image/png,image/jpeg" onchange={uploadAvatar} disabled={avatarBusy}/><span class="form-note">PNG or JPEG, up to 2 MB. We remove embedded metadata when saving.</span>{#if avatarMessage}<span class="form-note" aria-live="polite">{avatarMessage}</span>{/if}{#if avatarPreview}<img src={avatarPreview} class="avatar-upload-preview" alt=""/><button type="button" class="text-link" onclick={removeAvatar} disabled={avatarBusy}>Remove photo</button>{/if}</label><label>Link to one social profile · optional<input class="field" type="url" bind:value={identity_url} placeholder="https://instagram.com/…" /></label>
		<fieldset><legend>How should requests work?</legend><div class="choice-row"><label class:chosen={mode==='fixed'}><input type="radio" bind:group={mode} value="fixed"/> I’ll set a price</label><label class:chosen={mode==='offer'}><input type="radio" bind:group={mode} value="offer"/> Let people make an offer</label></div></fieldset>
		{#if mode==='fixed'}<label>Your price for 30 minutes<div class="money-input"><span>{currencySymbol(currency).trim()}</span><input type="number" bind:value={amount} inputmode="decimal" min="1" step="0.01" required /></div></label>{/if}
		<fieldset><legend>Available conversation lengths</legend><div class="duration-row">{#each [15,30,60] as d}<label class:selected={durations.includes(d)}><input type="checkbox" checked={durations.includes(d)} onchange={()=>toggle(d)}/> {d} min</label>{/each}</div></fieldset>
		<label>Your timezone<select class="field" bind:value={timezone}>{#each timezoneOptions(timezone) as zone}<option value={zone}>{zone}</option>{/each}</select></label>
		<label class="check-line"><input type="checkbox" bind:checked={paused}/> Pause new booking requests</label>
		<div class="notice notice-warning">Paused means your page stays up but nobody can book. Bookings you already have stay in place.</div>
		<div class="link-editor-save"><span>{dirty?'YOUR PUBLIC PAGE HAS NOT CHANGED YET':'YOUR PUBLIC PAGE MATCHES THIS EDITOR'}</span><button class="button" type="submit" disabled={saving || !dirty || durations.length===0}>{saving?'Saving…':'Save changes'} <span>↗</span></button></div>
		{#if message && (!dirty || saveState==='error')}<p class:notice-warning={saveState==='error'} class:notice-info={saveState!=='error'} class="notice" aria-live="polite">{message}</p>{/if}
	</form><div class="link-editor-preview" class:mobile-hidden={activeView==='edit'}><div class="link-editor-preview-head"><p class="eyebrow">Live private preview</p><span>{dirty?'DRAFT / NOT PUBLISHED':'MATCHES PUBLIC PAGE'}</span></div><PublicPersonPage person={previewPerson} preview={true}/></div></div>{/if}
</section>
