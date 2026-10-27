(function(){
  "use strict";
  var macros={"\\mathbbm":"\\mathbb{#1}","\\mathds":"\\mathbb{#1}","\\bm":"\\boldsymbol{#1}","\\hbox":"\\text{#1}","\\mbox":"\\text{#1}","\\ensuremath":"#1","\\xspace":"","\\protect":"","\\lefteqn":"#1","\\nobreak":"","\\displaylimits":"","\\eqref":"(\\text{#1})","\\ref":"\\text{#1}","\\label":"","\\textsc":"\\text{#1}","\\textsl":"\\textit{#1}","\\llbracket":"[\\![","\\rrbracket":"]\\!]","\\coloneqq":"\\mathrel{\\vcenter{:}}=","\\eqqcolon":"=\\mathrel{\\vcenter{:}}","\\EuScript":"\\mathscr","\\norm":"\\left\\lVert #1\\right\\rVert","\\abs":"\\left\\lvert #1\\right\\rvert","\\qedhere":"","\\xlongrightarrow":"\\xrightarrow","\\xlongleftarrow":"\\xleftarrow","\\fint":"\\int","\\makebox":"\\text"};
  function render(el){
    if(el.classList.contains("done")||!window.katex)return;
    var tex=el.textContent;
    try{katex.render(tex,el,{displayMode:el.classList.contains("display"),throwOnError:false,strict:"ignore",trust:false,macros:Object.assign({},macros),maxExpand:2000});el.classList.add("done");el.setAttribute("data-tex",tex)}catch(e){}
  }
  function maths(){
    var els=document.querySelectorAll(".math:not(.done)");
    if(!("IntersectionObserver" in window)){els.forEach(render);return}
    var io=new IntersectionObserver(function(es){es.forEach(function(e){if(e.isIntersecting){io.unobserve(e.target);render(e.target)}})},{rootMargin:"800px 0px"});
    els.forEach(function(el){io.observe(el)});
    document.addEventListener("toggle",function(e){if(e.target.tagName==="DETAILS")e.target.querySelectorAll(".math:not(.done)").forEach(render)},true);
  }
  function theme(){
    var b=document.querySelector(".theme");if(!b)return;
    b.addEventListener("click",function(){
      var r=document.documentElement,cur=r.dataset.theme||(matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light"),next=cur==="dark"?"light":"dark";
      r.dataset.theme=next;try{localStorage.setItem("theme",next)}catch(e){}
    });
  }
  function menu(){
    var b=document.querySelector(".menu"),n=document.getElementById("nav");if(!b||!n)return;
    b.addEventListener("click",function(){var o=n.classList.toggle("open");b.setAttribute("aria-expanded",o)});
  }
  var index=null;
  function search(){
    var q=document.getElementById("q"),hits=document.getElementById("hits");if(!q)return;
    var sel=-1;
    function load(){if(index)return Promise.resolve(index);return fetch("/search.json").then(function(r){return r.json()}).then(function(d){index=d.map(function(e){e.l=(e.t+" "+(e.s||"")).toLowerCase();return e});return index})}
    function esc(s){return s.replace(/[&<>"]/g,function(c){return{"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[c]})}
    function show(){
      var v=q.value.trim().toLowerCase();
      if(v.length<2){hits.hidden=true;return}
      load().then(function(ix){
        var ws=v.split(/\s+/),out=[];
        for(var i=0;i<ix.length&&out.length<30;i++){var e=ix[i],ok=true;for(var j=0;j<ws.length;j++)if(e.l.indexOf(ws[j])<0){ok=false;break}if(ok)out.push(e)}
        sel=-1;
        hits.innerHTML=out.length?out.map(function(e){return '<li><a href="'+esc(e.u)+'"><span class="dot k-'+(e.k==="paper"?"other":esc(e.k))+'"></span> '+esc(e.t)+'<small>'+(e.k==="paper"?"paper · ":"")+esc(e.s||"")+'</small></a></li>'}).join(""):'<li><a>No match</a></li>';
        hits.hidden=false;
      });
    }
    q.addEventListener("input",show);
    q.addEventListener("focus",function(){load();if(q.value)show()});
    q.addEventListener("keydown",function(e){
      var ls=hits.querySelectorAll("li");
      if(e.key==="ArrowDown"||e.key==="ArrowUp"){e.preventDefault();if(!ls.length)return;if(sel>=0)ls[sel].classList.remove("sel");sel=(sel+(e.key==="ArrowDown"?1:ls.length-1))%ls.length;ls[sel].classList.add("sel");ls[sel].scrollIntoView({block:"nearest"})}
      else if(e.key==="Enter"){var a=(sel>=0?ls[sel]:ls[0]);a=a&&a.querySelector("a[href]");if(a)location.href=a.href}
      else if(e.key==="Escape"){hits.hidden=true;q.blur()}
    });
    document.addEventListener("click",function(e){if(!e.target.closest(".search"))hits.hidden=true});
    document.addEventListener("keydown",function(e){if(e.key==="/"&&document.activeElement!==q&&!/INPUT|TEXTAREA/.test(document.activeElement.tagName)){e.preventDefault();q.focus()}});
  }
  function filter(){
    var f=document.querySelector(".filter");if(!f)return;
    var secs=document.querySelectorAll(".kpaper");
    f.addEventListener("input",function(){
      var v=f.value.trim().toLowerCase();
      secs.forEach(function(s){
        var head=s.querySelector("h3").textContent.toLowerCase(),inHead=!v||head.indexOf(v)>=0,any=false;
        s.querySelectorAll("li").forEach(function(li){var ok=inHead||li.textContent.toLowerCase().indexOf(v)>=0;li.hidden=!ok;if(ok)any=true});
        s.hidden=!any;
      });
    });
  }
  function toc(){
    var links=document.querySelectorAll(".toc a");if(!links.length||!("IntersectionObserver" in window))return;
    var by={};links.forEach(function(a){by[a.getAttribute("href").slice(1)]=a});
    var cur=null;
    var io=new IntersectionObserver(function(es){es.forEach(function(e){if(e.isIntersecting&&by[e.target.id]){if(cur)cur.classList.remove("on");cur=by[e.target.id];cur.classList.add("on")}})},{rootMargin:"-60px 0px -70% 0px"});
    document.querySelectorAll(".pmain>.item").forEach(function(el){io.observe(el)});
  }
  function open(){
    var h=location.hash.slice(1);if(!h)return;var el=document.getElementById(h);
    if(el&&el.tagName==="DETAILS"){el.open=true;el.scrollIntoView()}
  }
  function start(){maths();theme();menu();search();filter();toc();open();window.addEventListener("hashchange",open)}
  if(document.readyState==="loading")document.addEventListener("DOMContentLoaded",start);else start();
})();
