<!doctype html>
<html lang="en">
  <head>
    <script>self["MonacoEnvironment"] = (function (paths) {
          return {
            globalAPI: false,
            getWorkerUrl : function (moduleId, label) {
              var result =  paths[label];
              if (/^((http:)|(https:)|(file:)|(\/\/))/.test(result)) {
                var currentUrl = String(window.location);
                var currentOrigin = currentUrl.substr(0, currentUrl.length - window.location.hash.length - window.location.search.length - window.location.pathname.length);
                if (result.substring(0, currentOrigin.length) !== currentOrigin) {
                  var js = '/*' + label + '*/importScripts("' + result + '");';
                  var blob = new Blob([js], { type: 'application/javascript' });
                  return URL.createObjectURL(blob);
                }
              }
              return result;
            }
          };
        })({
  "editorWorkerService": "/monacoeditorwork/editor.worker.bundle.js"
});</script>

    <meta charset="UTF-8" />
    <link rel="icon" type="image/x-icon" href="/favicon.ico" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <script type="module" src="/assets/app_config_user.js"></script>
    <script type="module" src="/assets/app_config.js"></script>
    <script type="module" src="/assets/checkChrome.js"></script>
    <title>DeepFlow</title>
    <script type="module" crossorigin src="/assets/index-Bx61EpAi.js"></script>
    <link rel="modulepreload" crossorigin href="/assets/_plugin-vue_export-helper-CRt-r6Cj.js">
    <link rel="modulepreload" crossorigin href="/assets/rolldown-runtime-C5c2KzVm.js">
    <link rel="modulepreload" crossorigin href="/assets/@codemirror-vendor-B_y_qXqw.js">
    <link rel="modulepreload" crossorigin href="/assets/@vue-vendor-BiAdlnhr.js">
    <link rel="modulepreload" crossorigin href="/assets/i18n-hpZHBxA1.js">
    <link rel="modulepreload" crossorigin href="/assets/d3-vendor-Hbl8Isc-.js">
    <link rel="modulepreload" crossorigin href="/assets/langium-vendor-BMu4Nv9e.js">
    <link rel="modulepreload" crossorigin href="/assets/browser-vendor-DzueQaQs.js">
    <link rel="modulepreload" crossorigin href="/assets/algorithm-vendor-F4PoJiD8.js">
    <link rel="modulepreload" crossorigin href="/assets/other-vendor-yQZZqaf5.js">
    <link rel="modulepreload" crossorigin href="/assets/analysis-vendor-Bi61xtv2.js">
    <link rel="modulepreload" crossorigin href="/assets/datetime-vendor-DytgT_Kc.js">
    <link rel="modulepreload" crossorigin href="/assets/vue-vendor-Ri77IBGf.js">
    <link rel="modulepreload" crossorigin href="/assets/useDeleteConfirm-hLNoEiWJ.js">
    <link rel="modulepreload" crossorigin href="/assets/logger-CdqIyHgQ.js">
    <link rel="modulepreload" crossorigin href="/assets/utils-vendor-1-BVqXzy4r.js">
    <link rel="modulepreload" crossorigin href="/assets/tool-BlUmHaKI.js">
    <link rel="modulepreload" crossorigin href="/assets/unit-BhiYBWRD.js">
    <link rel="modulepreload" crossorigin href="/assets/ag-grid-vendor-DDlTIFNG.js">
    <link rel="modulepreload" crossorigin href="/assets/vueuse-vendor-BgqSJQ2H.js">
    <link rel="modulepreload" crossorigin href="/assets/AgTable-CdPucxnz.js">
    <link rel="modulepreload" crossorigin href="/assets/checkCircleFill-CM3qTuGB.js">
    <link rel="modulepreload" crossorigin href="/assets/checkCircleFillLarge-Co48GyHL.js">
    <link rel="modulepreload" crossorigin href="/assets/close-BWz8PyRU.js">
    <link rel="modulepreload" crossorigin href="/assets/closeCircleFill-uQeERiVP.js">
    <link rel="modulepreload" crossorigin href="/assets/closeCircleFillLarge-D8hf2K4t.js">
    <link rel="modulepreload" crossorigin href="/assets/exclamationCircleFillLarge-65EjcEIs.js">
    <link rel="modulepreload" crossorigin href="/assets/loading-CGFZjCpK.js">
    <link rel="modulepreload" crossorigin href="/assets/loadingSmall-B44kuL6Y.js">
    <link rel="modulepreload" crossorigin href="/assets/user-BYCsVfkq.js">
    <link rel="modulepreload" crossorigin href="/assets/mergeAppConfig-DgJ9youI.js">
    <link rel="modulepreload" crossorigin href="/assets/data-vendor-Bni7qkoM.js">
    <link rel="modulepreload" crossorigin href="/assets/http-vendor-Be4sc9Qt.js">
    <link rel="modulepreload" crossorigin href="/assets/crypto-vendor-CP3gkDJw.js">
    <link rel="modulepreload" crossorigin href="/assets/api-BHRiXTKO.js">
    <link rel="modulepreload" crossorigin href="/assets/tanstack-vendor-gKGA18yS.js">
    <link rel="modulepreload" crossorigin href="/assets/validate-vendor-BKdy1PxM.js">
    <link rel="modulepreload" crossorigin href="/assets/base-D40twt7B.js">
    <link rel="modulepreload" crossorigin href="/assets/queryClient-CsIETJgP.js">
    <link rel="modulepreload" crossorigin href="/assets/query-qivibuFx.js">
    <link rel="modulepreload" crossorigin href="/assets/time-BEDUuH1L.js">
    <link rel="modulepreload" crossorigin href="/assets/app-Bn0Tv23G.js">
    <link rel="modulepreload" crossorigin href="/assets/resourcePool-CZK4qGMI.js">
    <link rel="modulepreload" crossorigin href="/assets/resource-BhPQNbKN.js">
    <link rel="modulepreload" crossorigin href="/assets/window-BHFLMnRd.js">
    <link rel="modulepreload" crossorigin href="/assets/cytoscape-vendor-2pgbSVu6.js">
    <link rel="modulepreload" crossorigin href="/assets/markdown-vendor-nK0WMMW1.js">
    <link rel="modulepreload" crossorigin href="/assets/ui-effects-vendor-Bcv0omCm.js">
    <link rel="modulepreload" crossorigin href="/assets/echarts-vendor-DdBe8bGU.js">
    <link rel="modulepreload" crossorigin href="/assets/vue-ext-vendor-CK3jdfqA.js">
    <link rel="stylesheet" crossorigin href="/assets/browser-vendor-R_POiqJ_.css">
    <link rel="stylesheet" crossorigin href="/assets/other-vendor-lMmXi5eQ.css">
    <link rel="stylesheet" crossorigin href="/assets/vue-vendor-dZcp7gjq.css">
    <link rel="stylesheet" crossorigin href="/assets/ag-grid-vendor-Bi1G80_5.css">
    <link rel="stylesheet" crossorigin href="/assets/AgTable-DrqnQ9l7.css">
    <link rel="stylesheet" crossorigin href="/assets/markdown-vendor-Cyv6lCwm.css">
    <link rel="stylesheet" crossorigin href="/assets/ui-effects-vendor-1oWo33m3.css">
    <link rel="stylesheet" crossorigin href="/assets/vue-ext-vendor-BLlMA27I.css">
    <link rel="stylesheet" crossorigin href="/assets/index-x32YJsIE.css">
    <link rel="stylesheet" crossorigin href="/assets/highlight-vendor-B-oHczHB.css">
  </head>

  <body>
    <div id="app"></div>
  </body>
</html>
