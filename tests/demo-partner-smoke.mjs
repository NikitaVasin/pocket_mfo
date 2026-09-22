import { chromium, expect } from '@playwright/test';
import { mkdtempSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync, spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { createServer as tcpServer } from 'node:net';

const dir=mkdtempSync(join(tmpdir(),'demo-partner-browser-'));
let child,browser,page;
const received=[];
const receiver=createServer(async(req,res)=>{let data='';for await(const chunk of req)data+=chunk;received.push(JSON.parse(data));res.writeHead(200);res.end('{}');});
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
try {
 await new Promise(resolve=>receiver.listen(0,'127.0.0.1',resolve));
 const probe=tcpServer();await new Promise(resolve=>probe.listen(0,'127.0.0.1',resolve));const port=probe.address().port;await new Promise(resolve=>probe.close(resolve));
 const binary=join(dir,'partner');execFileSync('go',['build','-o',binary,'./example/demopartner'],{env:{...process.env,GOTOOLCHAIN:'auto'},stdio:'inherit'});
 child=spawn(binary,[],{env:{...process.env,PARTNER_ADDR:`127.0.0.1:${port}`,PARTNER_DATA:join(dir,'data.json'),POSTBACK_URL:`http://127.0.0.1:${receiver.address().port}/post`},stdio:'ignore'});
 const base=`http://127.0.0.1:${port}`;let healthy=false;for(let i=0;i<100;i++){if(await fetch(base+'/api/health').then(r=>r.ok).catch(()=>false)){healthy=true;break;}await pause(100);}if(!healthy)throw new Error('Partner did not start');
 browser=await chromium.launch();page=await browser.newPage({viewport:{width:1320,height:980}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base);await expect(page.getByText('Пока нет переходов')).toBeVisible();
 const click=await fetch(base+'/click?subid=browser-test-click',{redirect:'manual'});expect(click.status).toBe(302);expect(click.headers.get('location')).toBe('https://www.google.com/');
 await page.getByRole('button',{name:'Обновить',exact:true}).click();await expect(page.locator('tbody tr')).toHaveCount(1);
 await page.getByRole('button',{name:'Изменить',exact:true}).click();await page.locator('#status').selectOption('hold');await page.getByLabel('Ваше вознаграждение').fill('350.25');await page.getByRole('button',{name:'Сохранить и отправить'}).click();
 await expect(page.getByText('Доставлен',{exact:false})).toBeVisible();expect(received[0].status).toBe('hold');expect(received[0].subid).toBe('browser-test-click');expect(received[0].amount).toBe('350.25');
 await page.getByLabel('Поиск заявки').fill('missing');await expect(page.locator('tbody tr')).toHaveCount(0);await page.getByLabel('Поиск заявки').fill('');await expect(page.locator('tbody tr')).toHaveCount(1);
 mkdirSync('test-results',{recursive:true});
 for(const theme of ['light','dark']){
  if(await page.locator('html').getAttribute('data-theme')!==theme)await page.getByRole('button',{name:'Переключить тему'}).click();
  await page.screenshot({path:`test-results/demo-partner-${theme}.png`,fullPage:true});
  await page.getByRole('button',{name:'Настройки постбеков',exact:true}).click();await expect(page.getByLabel('Секрет',{exact:true})).toHaveValue('');await expect(page.getByText('Секрет сохранён.',{exact:false})).toBeVisible();
  await page.screenshot({path:`test-results/demo-partner-settings-${theme}.png`});
  await page.getByRole('button',{name:'Сохранить настройки',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);
 }
 await page.setViewportSize({width:390,height:844});await page.getByRole('button',{name:'Настройки постбеков',exact:true}).click();await expect(page.getByLabel('URL получателя')).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.screenshot({path:'test-results/demo-partner-mobile.png',fullPage:true});
 await page.getByRole('button',{name:'Закрыть',exact:true}).click();await page.reload();await expect(page.locator('tbody tr')).toHaveCount(1);expect(errors).toEqual([]);
 console.log('Demo partner browser smoke passed: redirect, status/postback, search, settings, light/dark and mobile.');
} catch(error) {
 if(page){console.error(await page.locator('body').ariaSnapshot());mkdirSync('test-results',{recursive:true});await page.screenshot({path:'test-results/demo-partner-failure.png'});}
 throw error;
} finally {
 await browser?.close();await new Promise(resolve=>receiver.close(resolve));
 if(child&&child.exitCode===null){const stopped=new Promise(resolve=>child.once('exit',resolve));child.kill('SIGTERM');await stopped;}
 rmSync(dir,{recursive:true,force:true});
}
