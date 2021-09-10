const exam = new Vue({
    el: "#exam",
    data: {
        allExams: {},
        questions: [],
        curQ: 0, // Index of current question.
        curExam: "", // Name of current exam.
    },
    computed: {
        question: function () {
            return this.questions[this.curQ];
        },
        exam: function() {
            return this.allExams[this.curExam];
        },
        number: function () {
            return this.curQ + 1;
        },
    },
    methods: {
        optionClass: function (i /* Index of option. */) {
            const className = "list-group-item list-group-item-action list-group-item";
            const num = this.toNumber(i);
            if (this.questions[this.curQ].userChoice == num) {
                if (this.question.answer == num) {
                    return className + "-success";
                } else {
                    return className + "-danger";
                }
            }
            return className;
        },
        pageClass: function (i /* Index of page. */) {
            var pageClass = "page btn btn"
            if (this.curQ != i) {
                pageClass += "-outline";
            }
            q = this.questions[i]
            if (q.userChoice == 0) {
                return pageClass + "-primary";
            }
            if (q.userChoice == q.answer) {
                return pageClass + "-success";
            }
            return pageClass + "-danger";
        },
        toNumber: function (i) {
            return i + 1;
        },
        pageLink: function (i /* Index of page */) {
            return `#${this.curExam}#${i + 1}`
        },
    }
})

function loadQuestions() {
    const hash = document.location.hash;
    parts = hash.split("#")
    var page = "";
    var curQ = 0;
    if (parts.length >= 2) {
        page = parts[1]
    }
    if (parts.length >= 3) {
        num = parseInt(parts[2]);
        curQ = num - 1;
    }
    console.log(`Load with hash: ${page} / ${curQ + 1}`)

    exam.curExam = page;
    exam.questions = [];

    // Update document direction according to the current exam.
    const htmlElem = document.getElementsByTagName("html")[0];
    htmlElem.setAttribute("lang", exam.exam.lang);
    htmlElem.setAttribute("dir", exam.exam.dir);

    const data = JSON.parse(window.localStorage.getItem(exam.curExam));
    if (data != null) {
        console.log("Getting data from local storage");
        exam.questions = data;
        exam.curQ = curQ;
        return;
    }
    console.log("Fetching data");

    var path = `${mountPath()}/exams/${exam.curExam}.json`;
    fetch(path).then(res => {
        res.json().then(data => {
            for (let i = 0; i < data.length; i++) {
                let item = data[i];
                item.userChoice = 0;
                item.i = i;
                exam.questions.push(item)
            }
            exam.curQ = curQ;
        });
    });
}

function showQuestion(i) {
    if (isNaN(i) || i < 0 || i >= exam.questions.length) {
        i = 0;
    }
    document.location.hash = exam.pageLink(i);
    exam.curQ = i;
    const page = document.getElementById(`page-${i}`).getClientRects()[0];
    const screenWidth = document.body.clientWidth;
    document.getElementById("pages").scrollBy({
        left: page.left + page.width / 2 - screenWidth / 2,
        behavior: 'smooth',
    });
    console.log(`Changed to ${i + 1}`);
}

function showExam(name) {
    exam.curExam = name;
    document.location.hash = exam.pageLink(0);
    loadQuestions();
}

// User chooses an answer.
function onChoose(i) {
    const num = i + 1;
    exam.questions[exam.curQ].userChoice = num;
    window.localStorage.setItem(exam.curExam, JSON.stringify(exam.questions))

    // If the answer is correct and there are more questions, go to the next question.
    if (exam.question.userChoice == exam.question.answer) {
        setTimeout(() => showQuestion(exam.curQ + 1), 150);
    }
}

// User asked to reset.
function onReset() {
    window.localStorage.removeItem(exam.curExam);
    loadQuestions();
}

function mountPath() {
    return document.location.pathname.replace(/index.html$/, '').replace(/\/$/, '');
}

window.addEventListener("keydown", event => {
    const key = event.key;

    var diff = 1;
    if (exam.exam.dir == "rtl") {
        diff = -1
    }

    // From https://keycode.info/
    if (key === "ArrowDown" || key == "ArrowRight") {
        showQuestion(exam.curQ + diff)
        return;
    }
    if (key === "ArrowUp" || key == "ArrowLeft") {
        showQuestion(exam.curQ - diff)
        return;
    }
    if (key >= "1" && key <= "9") {
        onChoose(parseInt(key) - diff);
        return;
    }
});

function loadExams() {
    console.log("Fetching exams index");
    fetch("exams.json").then(res => {
        res.json().then(data => {
            exam.allExams = data;
            loadQuestions();
        });
    });
}

loadExams();