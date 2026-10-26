// Render the real question form on the hidden GPU screen with the Vite test server.
const {app,BrowserWindow}=require('electron');
const assert=require('node:assert/strict');
app.whenReady().then(async()=>{try{
 const win=new BrowserWindow({width:800,height:900});
 await win.loadURL('http://127.0.0.1:5189/tests/question-height.html');
 const r=await win.webContents.executeJavaScript('window.heightCheck()');
 assert.ok(r.page>0&&r.page<300,'the short tab is short: '+r.page);
 assert.ok(r.pages<=r.page+1,'the form is as tall as the shown tab, not the tallest one: '+JSON.stringify(r));
 const t=await win.webContents.executeJavaScript('window.heightCheck(1)');
 assert.ok(t.page>r.page*2&&Math.abs(t.pages-t.page)<=1,'the tall tab grows the form to fit it: '+JSON.stringify(t));
 console.log('PASS: the question form is only as tall as the shown tab');
 app.exit(0);
}catch(e){console.error(e);app.exit(1)}});
