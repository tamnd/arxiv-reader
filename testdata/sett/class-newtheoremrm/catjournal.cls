\NeedsTeXFormat{LaTeX2e}
\ProvidesClass{catjournal}
\LoadClass{article}
\def\newtheoremrm#1{\@ifnextchar[{\@newthmrm{#1}}{\@newthmrm{#1}[]}}
\def\@newthmrm#1[#2]#3{\newtheorem{#1}{#3}}
\newtheoremrm{remark}{Remark}
