// Render the real virtual list on the hidden GPU screen with the Vite test server.
const {app,BrowserWindow}=require('electron');
const assert=require('node:assert/strict');
app.whenReady().then(async()=>{try{
 const win=new BrowserWindow({width:600,height:400});
 await win.loadURL('http://127.0.0.1:5189/tests/scroll-near.html');
 const result=await win.webContents.executeJavaScript('window.scrollCheck()');
 assert.ok(result.gap>=2&&result.gap<=4,'the view is really a few pixels above bottom');
 assert.equal(result.near,true,'a small keyboard scroll must keep following');
 assert.equal(result.far,false,'reading well above bottom must stop following');
 console.log('PASS: keyboard scrolling uses the same near-bottom allowance as the wheel');
 app.exit(0);
}catch(e){console.error(e);app.exit(1)}});
