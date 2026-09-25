import { Buffer } from 'node:buffer';
import { deflateSync } from 'node:zlib';
import { error, redirect } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { env } from '$env/dynamic/private';

const glyphs: Record<string, string[]> = {
  A:['01110','10001','10001','11111','10001','10001','10001'],B:['11110','10001','10001','11110','10001','10001','11110'],C:['01111','10000','10000','10000','10000','10000','01111'],D:['11110','10001','10001','10001','10001','10001','11110'],E:['11111','10000','10000','11110','10000','10000','11111'],F:['11111','10000','10000','11110','10000','10000','10000'],G:['01111','10000','10000','10111','10001','10001','01111'],H:['10001','10001','10001','11111','10001','10001','10001'],I:['11111','00100','00100','00100','00100','00100','11111'],J:['00111','00010','00010','00010','10010','10010','01100'],K:['10001','10010','10100','11000','10100','10010','10001'],L:['10000','10000','10000','10000','10000','10000','11111'],M:['10001','11011','10101','10101','10001','10001','10001'],N:['10001','11001','10101','10011','10001','10001','10001'],O:['01110','10001','10001','10001','10001','10001','01110'],P:['11110','10001','10001','11110','10000','10000','10000'],Q:['01110','10001','10001','10001','10101','10010','01101'],R:['11110','10001','10001','11110','10100','10010','10001'],S:['01111','10000','10000','01110','00001','00001','11110'],T:['11111','00100','00100','00100','00100','00100','00100'],U:['10001','10001','10001','10001','10001','10001','01110'],V:['10001','10001','10001','10001','10001','01010','00100'],W:['10001','10001','10001','10101','10101','10101','01010'],X:['10001','10001','01010','00100','01010','10001','10001'],Y:['10001','10001','01010','00100','00100','00100','00100'],Z:['11111','00001','00010','00100','01000','10000','11111'],
  '0':['01110','10001','10011','10101','11001','10001','01110'],'1':['00100','01100','00100','00100','00100','00100','01110'],'2':['01110','10001','00001','00010','00100','01000','11111'],'3':['11110','00001','00001','01110','00001','00001','11110'],'4':['00010','00110','01010','10010','11111','00010','00010'],'5':['11111','10000','10000','11110','00001','00001','11110'],'6':['01110','10000','10000','11110','10001','10001','01110'],'7':['11111','00001','00010','00100','01000','01000','01000'],'8':['01110','10001','10001','01110','10001','10001','01110'],'9':['01110','10001','10001','01111','00001','00001','01110'],
  '.':['00000','00000','00000','00000','00000','00110','00110'],',':['00000','00000','00000','00000','00110','00110','00100'],':':['00000','00110','00110','00000','00110','00110','00000'],'/':['00001','00010','00010','00100','01000','01000','10000'],'-':['00000','00000','00000','11111','00000','00000','00000'],'\'':['00100','00100','01000','00000','00000','00000','00000'],'?':['01110','10001','00001','00010','00100','00000','00100'],'!':['00100','00100','00100','00100','00100','00000','00100'],'&':['01100','10010','10100','01000','10101','10010','01101']
};

function crc32(data: Buffer) { let crc=0xffffffff; for(const byte of data){crc^=byte;for(let i=0;i<8;i++)crc=(crc>>>1)^((crc&1)?0xedb88320:0)} return (crc^0xffffffff)>>>0; }
function chunk(type:string,data:Buffer) { const name=Buffer.from(type); const out=Buffer.alloc(12+data.length); out.writeUInt32BE(data.length,0); name.copy(out,4); data.copy(out,8); out.writeUInt32BE(crc32(out.subarray(4,8+data.length)),8+data.length); return out; }

export const GET: RequestHandler = async ({ params, fetch }) => {
  const handle=params.handle.toLowerCase();
  if(!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(handle)||!/^[0-9]+$/.test(params.version)) throw error(404,'Preview unavailable.');
  const apiBase=env.API_INTERNAL_BASE||'http://127.0.0.1:8081';
  let person: {name:string;handle:string;mode:string;paused:boolean;base_30_minor:number;public_version?:number;currency?:string};
  try { const response=await fetch(`${apiBase}/api/v1/people/${encodeURIComponent(handle)}`); if(!response.ok)throw error(404,'Preview unavailable.'); person=await response.json(); }
  catch(cause){if(cause&&typeof cause==='object'&&'status'in cause)throw cause;throw error(503,'Preview temporarily unavailable.');}
  // Only the current version is rendered; stale or invented versions redirect,
  // so arbitrary version numbers cannot force repeated fresh renders.
  const current=Math.max(1,Number(person.public_version||1));
  if(Number(params.version)!==current) throw redirect(302,`/og/${encodeURIComponent(handle)}/${current}.png`);
  const width=1200,height=630,stride=width*4+1,raw=Buffer.alloc(stride*height);
  const pixel=(x:number,y:number,color:[number,number,number,number])=>{if(x<0||y<0||x>=width||y>=height)return;const o=y*stride+1+x*4;raw[o]=color[0];raw[o+1]=color[1];raw[o+2]=color[2];raw[o+3]=color[3]};
  const rect=(x:number,y:number,w:number,h:number,c:[number,number,number,number])=>{for(let yy=y;yy<y+h;yy++)for(let xx=x;xx<x+w;xx++)pixel(xx,yy,c)};
  rect(0,0,width,height,[242,240,233,255]);
  rect(72,82,1056,2,[23,24,23,255]);rect(72,545,1056,2,[23,24,23,255]);
  const draw=(text:string,x:number,y:number,scale:number,color:[number,number,number,number],maxChars:number)=>{let normalized=text.normalize('NFKD').replace(/[\u0300-\u036f]/g,'').replace(/[^\x20-\x7e]/g,'').toUpperCase().slice(0,maxChars);for(let ci=0;ci<normalized.length;ci++){const char=normalized[ci];if(char===' ')continue;const glyph=glyphs[char]||glyphs['?'];for(let gy=0;gy<7;gy++)for(let gx=0;gx<5;gx++)if(glyph[gy][gx]==='1')rect(x+ci*6*scale+gx*scale,y+gy*scale,scale,scale,color)}};
  const dark:[number,number,number,number]=[23,24,23,255],muted:[number,number,number,number]=[85,90,83,255],red:[number,number,number,number]=[180,58,48,255];
  draw('WANTMYTIME / PERSONAL BOOKING',72,104,4,dark,37);
  draw('A CONVERSATION',72,215,13,dark,20);
  const foldedName=(person.name||'').normalize('NFKD').replace(/[\u0300-\u036f]/g,'');
  const rasterName=/^[\x20-\x7e]+$/.test(foldedName)?foldedName:person.handle;
  draw(`WITH ${rasterName}`,72,335,11,dark,19);
  let siteHost='wantmytime.com';try{siteHost=new URL(env.PUBLIC_APP_ORIGIN||'https://wantmytime.com').host}catch{}
  const price=person.mode!=='offer'&&!person.paused?`${(person.currency||'NGN').replace(/[^A-Z]/g,'')} ${Math.round(Number(person.base_30_minor)/100).toLocaleString('en-US')} / 30 MIN`:'BOOK TIME ON YOUR TERMS';
  draw(price,72,462,5,red,38);draw(`${siteHost}/${person.handle}`,72,566,4,muted,36);
  const ihdr=Buffer.alloc(13);ihdr.writeUInt32BE(width,0);ihdr.writeUInt32BE(height,4);ihdr[8]=8;ihdr[9]=6;
  const png=Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]),chunk('IHDR',ihdr),chunk('IDAT',deflateSync(raw)),chunk('IEND',Buffer.alloc(0))]);
  return new Response(png,{headers:{'Content-Type':'image/png','Cache-Control':'public, max-age=86400, stale-while-revalidate=604800','ETag':`"${handle}-${params.version}-png"`,'X-Content-Type-Options':'nosniff'}});
};
