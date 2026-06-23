// Tint shell (`sh`) code fences with the four brand accents, cycling per block
// so consecutive blocks differ. The pygments shell lexer leaves commands as bare
// text (only whitespace/comment tokens get spans), so the blocks render in one
// drab grey; this assigns each block a `data-ev-accent` that extra.css keys the
// command colour off (comments stay muted). A MutationObserver re-runs the pass
// after zensical's instant navigation swaps in new page content.
(function () {
    var COLORS = ["magenta", "green", "cyan", "orange"];
    var next = 0;

    function paint() {
        document.querySelectorAll(".language-sh.highlight:not([data-ev-accent])").forEach(function (block) {
            block.setAttribute("data-ev-accent", COLORS[next % COLORS.length]);
            next++;
        });
    }

    paint();
    // childList/subtree only — setAttribute above mutates attributes, which this
    // observer ignores, so painting never re-triggers itself.
    new MutationObserver(paint).observe(document.body, { childList: true, subtree: true });
})();
