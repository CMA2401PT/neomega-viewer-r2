import type { MaterialInput } from "/r2/r2-materials.js";

export type SavedMaterial = Exclude<MaterialInput, File[]>;

// 浏览器不能按上次的本机路径重新读文件。保存用户选过的文件及目录相对路径，重开时可重新合成。
function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("neomega-r2-materials", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("selection");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

export async function loadMaterial(): Promise<SavedMaterial> {
  const db = await openDatabase();
  return new Promise((resolve, reject) => {
    const transaction = db.transaction("selection");
    transaction.oncomplete = transaction.onabort = () => db.close();
    const read = transaction.objectStore("selection").get("current");
    read.onsuccess = () => resolve(read.result ?? null);
    read.onerror = () => reject(read.error);
  });
}

export async function saveMaterial(input: SavedMaterial): Promise<void> {
  const db = await openDatabase();
  return new Promise((resolve, reject) => {
    const transaction = db.transaction("selection", "readwrite");
    const store = transaction.objectStore("selection");
    if (input) store.put(input, "current");
    else store.delete("current");
    transaction.oncomplete = () => {
      db.close();
      resolve();
    };
    transaction.onabort = () => {
      db.close();
      reject(transaction.error);
    };
  });
}
