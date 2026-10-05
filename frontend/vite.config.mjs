import { defineConfig, searchForWorkspaceRoot } from "vite";
import { realpathSync, readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL(".", import.meta.url));
const require=createRequire(import.meta.url);
const pdfRoot=dirname(require.resolve("pdfjs-dist/package.json"));
const pdfAssets=new Map();
for(const folder of ["cmaps","standard_fonts","wasm","iccs"]){
  for(const name of readdirSync(join(pdfRoot,folder))){
    pdfAssets.set(`/pdfjs-assets/${folder}/${name}`,readFileSync(join(pdfRoot,folder,name)));
  }
}
export default defineConfig({
  build:{rollupOptions:{input:{main:join(root,"index.html"),print:join(root,"print.html"),extract:join(root,"extract.html"),validate:join(root,"validate.html")}}},
  plugins:[{
    name:"local-pdfjs-assets",
    configureServer(server){server.middlewares.use((request,response,next)=>{
      const pathname=new URL(request.url??"/","http://localhost").pathname,data=pdfAssets.get(pathname);
      if(!data)return next();
      response.setHeader("Content-Type",pathname.endsWith(".wasm")?"application/wasm":"application/octet-stream");response.end(data);
    });},
    generateBundle(){for(const [path,source] of pdfAssets)this.emitFile({type:"asset",fileName:path.slice(1),source});},
  }],
  preview: {headers:{"Content-Security-Policy":"frame-ancestors 'none'","X-Frame-Options":"DENY"}},
  server: {
    headers:{"Content-Security-Policy":"frame-ancestors 'none'","X-Frame-Options":"DENY"},
    hmr: process.env.TROVE_TEST_URL ? false : undefined,
    fs: {
      allow: [
        searchForWorkspaceRoot(root),
        realpathSync(new URL("./node_modules", import.meta.url)),
      ],
    },
  },
});
