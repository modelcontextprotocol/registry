target = cheatsheet

.PHONY: all clean

all: $(target)

$(target): $(target).tex
	pdflatex $(target).tex

clean:
	rm -f $(target).pdf $(target).aux $(target).log
