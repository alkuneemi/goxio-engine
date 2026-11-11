import { portalCopy } from './portalCopy';
import { serverStatItems } from './serverStats';

function assertEqual(actual: unknown, expected: unknown, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)} but got ${String(actual)}`);
  }
}

const server = {
  containers: 112,
  databases: 20,
  volumes: 106,
  folders: 423,
  images: 46,
};

const ar = Object.fromEntries(serverStatItems(server, portalCopy('ar')).map(item => [item.key, item]));
const en = Object.fromEntries(serverStatItems(server, portalCopy('en')).map(item => [item.key, item]));

assertEqual(ar.volumes.value, 106, 'arabic volume count');
assertEqual(ar.volumes.label, 'وحدات التخزين', 'arabic volume label');
assertEqual(ar.folders.value, 423, 'arabic folder count');
assertEqual(ar.folders.label, 'المجلدات', 'arabic folder label');
assertEqual(en.volumes.value, 106, 'english volume count');
assertEqual(en.volumes.label, 'Volumes', 'english volume label');
assertEqual(en.folders.value, 423, 'english folder count');
assertEqual(en.folders.label, 'Folders', 'english folder label');

console.log('server stat checks ok');
