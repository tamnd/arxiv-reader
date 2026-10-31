% A synthetic class. Its option numberwithinsect numbers theorem within the
% section, and its option chapters is on by default.
\NeedsTeXFormat{LaTeX2e}
\ProvidesClass{journal}
\DeclareOption{numberwithinsect}{\let\numberwithinsect\relax}
\newif\if@stepsect
\DeclareOption{stepsect}{\@stepsecttrue}
\LoadClass{article}
\ProcessOptions
\RequirePackage{amsthm}
\newtheorem{theorem}{Theorem}
\newtheorem{lemma}[theorem]{Lemma}
\ifx\numberwithinsect\relax
  \@addtoreset{theorem}{section}
\fi
\if@stepsect
  \@addtoreset{theorem}{subsection}
\else
  \newcounter{note}[section]
\fi
