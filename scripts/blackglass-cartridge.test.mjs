import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync, spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { once } from 'node:events';

const helper = new URL('./blackglass-cartridge.py', import.meta.url).pathname;
test('deployment cartridge is external, pinned, validated and recorded without a source path', () => {
  const dir = mkdtempSync(join(tmpdir(), 'gosx-private-cartridge-'));
  try {
    const rom = Buffer.alloc(32768); let checksum = 0; for (let i=0x134; i<0x14d; i++) checksum=(checksum-1)&255; rom[0x14d]=checksum;
    const source=join(dir,'private.gbc'),root=join(dir,'dist');writeFileSync(source,rom);
    const hash=createHash('sha256').update(rom).digest('hex');
    const stage=expected=>spawnSync('python3',[helper,'--root',root,'--source',source,'--sha256',expected],{encoding:'utf8'});
    assert.notEqual(stage('0'.repeat(64)).status,0);assert.equal(existsSync(join(root,'runtime-assets/blackglass-cartridge.gbc')),false);
    const valid=stage(hash);assert.equal(valid.status,0,valid.stderr);
    assert.deepEqual(readFileSync(join(root,'runtime-assets/blackglass-cartridge.gbc')),rom);
    const receipt=readFileSync(join(root,'runtime-assets/blackglass-cartridge.json'),'utf8');assert.equal(receipt.includes(source),false);
    assert.deepEqual(JSON.parse(receipt),{sha256:hash,bytes:32768});
    rom[0x14d]^=1;writeFileSync(source,rom);const invalid=stage(createHash('sha256').update(rom).digest('hex'));assert.notEqual(invalid.status,0);assert.match(invalid.stderr,/checksum/);
    const inRepo=spawnSync('python3',[helper,'--root',root,'--source',helper,'--sha256',hash],{encoding:'utf8'});assert.notEqual(inRepo.status,0);assert.match(inRepo.stderr,/outside the repository/);
  } finally {rmSync(dir,{recursive:true,force:true});}
});

test('served cartridge verification identifies its client through a gateway and retains byte validation', async () => {
  const dir=mkdtempSync(join(tmpdir(),'gosx-cartridge-gateway-'));
  const rom=Buffer.alloc(32768),hash=createHash('sha256').update(rom).digest('hex'),root=join(dir,'dist');
  let corrupt=false;
  const server=createServer((req,res)=>{
    if(!req.headers['user-agent'] || req.headers['user-agent'].startsWith('Python-urllib/')){res.writeHead(403);res.end('Unidentified client');return;}
    if(req.url==='/api/blackglass-cartridge'){
      res.setHeader('Content-Type','application/json');res.end(JSON.stringify({available:true,bytes:rom.length,sha256:hash,url:'/api/blackglass-cartridge/rom?sha256='+hash}));
    }else if(req.url==='/api/blackglass-cartridge/rom?sha256='+hash){
      const bytes=Buffer.from(rom);if(corrupt)bytes[0]^=1;res.end(bytes);
    }else{res.writeHead(404);res.end();}
  });
  try{
    mkdirSync(join(root,'runtime-assets'),{recursive:true});
    writeFileSync(join(root,'runtime-assets/blackglass-cartridge.json'),JSON.stringify({sha256:hash,bytes:rom.length}));
    server.listen(0,'127.0.0.1');await once(server,'listening');
    const verify=()=>new Promise((resolve,reject)=>{
      const child=spawn('python3',[helper,'--root',root,'--verify-base','http://127.0.0.1:'+server.address().port]);let stderr='';
      child.stdout.resume();child.stderr.on('data',data=>stderr+=data);child.on('error',reject);child.on('close',status=>resolve({status,stderr}));
    });
    const valid=await verify();assert.equal(valid.status,0,valid.stderr);
    corrupt=true;const invalid=await verify();assert.notEqual(invalid.status,0);assert.match(invalid.stderr,/served cartridge bytes differ/);
  }finally{await new Promise(resolve=>server.close(resolve));rmSync(dir,{recursive:true,force:true});}
});
