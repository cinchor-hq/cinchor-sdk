/** Trace a single mint against a fresh chain to locate where the write is lost. */
import { readFileSync } from 'node:fs';
import { Wallet } from '@omne/sdk';
import { CapabilityRegistry, deriveCapabilityId } from '../dist/index.js';

const RPC = process.env.CINCHOR_RPC_URL!;
const ADDR = process.env.CINCHOR_CONTRACT_ADDRESS!;
const WPATH = process.env.CINCHOR_WALLETS!;
const w = JSON.parse(readFileSync(WPATH, 'utf8'));
const principal = Wallet.fromMnemonic(w.principal.mnemonic).getAccount(0);
const agent = Wallet.fromMnemonic(w.agent.mnemonic).getAccount(0);

const rpc = (m: string, p: unknown[]) =>
  fetch(RPC, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', method: m, params: p, id: 1 }) }).then((r) => r.json());

const reg = await CapabilityRegistry.connect({
  network: { name: 'ignis', chainId: 3, rpcUrl: RPC },
  contract: { name: 'cinchor_permissions', address: ADDR },
});

const burn = 'om1zmm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0dahk7mm0qdtuxap';
console.log('— contract address:', ADDR);
console.log('— get_status(burn) [contract callable?]:', await reg.getStatus(burn).then(String).catch((e) => 'ERR:' + e.message));
console.log('— principal:', principal.address);
console.log('— principal balance:', JSON.stringify((await rpc('omne_getBalance', [principal.address])).result ?? (await rpc('omne_getBalance', [principal.address])).error));
console.log('— principal nonce (omne_getNonce):', JSON.stringify((await rpc('omne_getNonce', [principal.address])).result));

const now = BigInt(Math.floor(Date.now() / 1000));
const capId = await deriveCapabilityId(principal.address, agent.address, 12345, Number(now));
console.log('— minting capId:', capId);

const receipt = await reg.mintPermission({
  signer: principal, capabilityId: capId, principal: principal.address, agent: agent.address,
  maxSpend: 100n, validUntil: now + 3600n, currentTime: now,
});
console.log('— mint receipt:', JSON.stringify(receipt));
if (receipt.transactionHash) console.log('TXHASH ' + receipt.transactionHash);

for (let i = 0; i < 12; i++) {
  await new Promise((r) => setTimeout(r, 2000));
  const s = await reg.getStatus(capId);
  console.log(`  t+${(i + 1) * 2}s get_status(cap)=${s}`);
  if (s === 1) { console.log('✅ capability active — maxSpend=', (await reg.getMaxSpend(capId)).toString()); break; }
}
