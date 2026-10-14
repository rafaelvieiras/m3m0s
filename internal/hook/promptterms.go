package hook

import (
	"strings"
	"unicode"
)

// genericWords are words that carry no topic: PT/EN function words plus the
// workflow vocabulary of "do the next thing" prompts (merge, deploy, seguir,
// commit...). A prompt made only of these ("pode fazer merge e deploy")
// matches generic memories from any project at a high cosine (D47), so it
// gets no injection.
var genericWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
a o as os um uma uns umas de do da dos das em no na nos nas por para pra com sem sob sobre entre
e ou mas que se como quando onde qual quais quem isso isto esse essa esses essas aquilo este esta
eu voce vc ele ela nos eles elas meu minha seu sua nosso nossa me te lhe
ja so tambem mais menos muito pouco bem mal aqui ali la agora depois antes ainda sempre nunca
sim nao ok certo beleza blz valeu obrigado obrigada por favor pfv
ser estar ter ha foi era sao esta estao tem tinha fazer faca faz feito fazendo fez
pode podem poder podemos posso vamos vai vao ir quero queria preciso precisa deve devemos
seguir segue siga continue continuar proximo proxima proximos seguinte sequencia passo item itens
ver veja olhar olha checar checa conferir validar valida verificar
comecar comeca comece iniciar inicia terminar termina acabar finalizar
mergear merge mergeie deploy deployar subir sobe suba commit commitar push pr pull request branch
rodar rode roda executar execute executa testar teste testes tests build compilar
pronto feito fim ultimo ultima primeiro primeira segundo segunda outro outra todo toda tudo nada algo
coisa coisas parte partes forma jeito tipo caso vez vezes
the a an and or but if then this that these those it its is are was were be been being to of in on at by for with from as
do does did done can could should would will shall may might must have has had
please yes no ok okay go ahead next continue proceed start run make let lets
merge deploy commit push pull request branch build test tests fix
`) {
		m[w] = true
	}
	return m
}()

// foldAccents maps the accented letters of Portuguese to ASCII so terms
// match regardless of accents.
var foldAccents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "ê", "e", "è", "e", "ë", "e",
	"í", "i", "î", "i", "ì", "i", "ï", "i",
	"ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
	"ú", "u", "û", "u", "ù", "u", "ü", "u", "ç", "c", "ñ", "n",
)

func foldText(s string) string { return foldAccents.Replace(strings.ToLower(s)) }

// specificTerms returns the topic-bearing terms of a prompt: lowercase,
// accent-folded words of 3+ characters (or any word with a digit, like "f3"
// or "d47") that are not generic. Duplicates are removed.
func specificTerms(prompt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.FieldsFunc(foldText(prompt), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	}) {
		w = strings.Trim(w, "-_")
		hasDigit := strings.IndexFunc(w, unicode.IsDigit) >= 0
		if (len([]rune(w)) < 3 && !hasDigit) || genericWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// sharesTerm reports whether content mentions at least one of the terms.
func sharesTerm(content string, terms []string) bool {
	c := foldText(content)
	for _, t := range terms {
		if strings.Contains(c, t) {
			return true
		}
	}
	return false
}
